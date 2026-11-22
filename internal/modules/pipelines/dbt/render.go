// Package dbt renders the deterministic dbt project that the execution engine
// runs for a published Pipeline DAG and parses the dbt run_results.json artifact
// used by run reconciliation.
//
// The renderer deliberately does not import the kestra package: Flow rendering
// is owned by the kestra adapter layer, and both layers only share the domain
// DAG. Files are produced in memory; uploading them and deleting them is the
// caller's concern.
package dbt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

// DefaultCatalog is the dbt target catalog used when the caller does not
// resolve one from configuration (local development default, design D9).
const DefaultCatalog = "memory"

// Stable rendering error codes. They are part of the error contract, so callers
// (and tests) branch on Code, never on Message.
const (
	CodeInvalidTenantID      = "invalid_tenant_id"
	CodeInvalidPipelineID    = "invalid_pipeline_id"
	CodeInvalidVersion       = "invalid_version"
	CodeInvalidTargetCatalog = "invalid_target_catalog"
	CodeInvalidTaskID        = "invalid_task_id"
	CodeDuplicateTaskID      = "duplicate_task_id"
	CodeUnsupportedTaskType  = "unsupported_task_type"
	// CodeInvalidSQLReference is also emitted by domain.ValidateDAG for the
	// authoring-time check; the renderer repeats it as defense in depth for
	// explicit out-of-scope catalog.schema.table references (design D9).
	CodeInvalidSQLReference = "invalid_sql_reference"
)

// RenderError is a stable, machine-readable dbt rendering failure. Message is
// human-readable, never contains full SQL and may change; Code must not.
type RenderError struct {
	Code    string
	Message string
}

// Error implements error.
func (e *RenderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Unwrap lets callers treat every rendering failure as an invalid argument
// while still extracting the stable Code with ErrorCode or errors.As.
func (e *RenderError) Unwrap() error { return domain.ErrInvalidArgument }

// ErrorCode returns the stable rendering code carried by err, or "" when err is
// not a rendering failure.
func ErrorCode(err error) string {
	var renderErr *RenderError
	if errors.As(err, &renderErr) {
		return renderErr.Code
	}
	return ""
}

// Project is the rendered dbt project. Files are ordered deterministically
// (dbt_project.yml, profiles.yml, then models by path). Schema is the derived
// tenant/Pipeline schema and ContentHash is sha256 over the sorted file
// path/content pairs.
type Project struct {
	Files       []File
	Schema      string
	ContentHash string
}

// File is one rendered file. Path always uses forward slashes and is relative
// to the dbt project root.
type File struct {
	Path    string
	Content []byte
}

const (
	dbtProjectName = "ontoaxis_pipeline"
	dbtProfileName = "ontoaxis_pipeline"
)

// dbtProjectFile is static: the profile name is fixed (design D9), the models
// directory is the only source path and every model is materialized as a table.
const dbtProjectFile = `name: ontoaxis_pipeline
version: "1.0.0"
config-version: 2
profile: ontoaxis_pipeline
model-paths: ["models"]
models:
  ontoaxis_pipeline:
    +materialized: table
`

// profilesFileTemplate only references connection values through env_var; no
// host, user, password or DSN literal is ever rendered. The target catalog and
// derived schema are controlled identifiers, not credentials, so they are
// rendered literally to pin the writable namespace (design D9).
const profilesFileTemplate = `ontoaxis_pipeline:
  target: prod
  outputs:
    prod:
      type: trino
      method: none
      host: "{{ env_var('DBT_TRINO_HOST') }}"
      port: "{{ env_var('DBT_TRINO_PORT') }}"
      user: "{{ env_var('DBT_TRINO_USER') }}"
      database: "%s"
      schema: "%s"
      threads: 2
`

// catalogPattern restricts the configured target catalog to the identifier
// subset we are willing to render into YAML and compare in SQL checks.
var catalogPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// qualifiedRelationPattern finds explicit catalog.schema.table references. A
// leading boundary avoids matching the tail of a longer dotted chain twice;
// overlapping chains are still rejected from their leftmost match.
var qualifiedRelationPattern = regexp.MustCompile(`(?i)([a-z_][a-z0-9_]*)\s*\.\s*([a-z_][a-z0-9_]*)\s*\.\s*([a-z_][a-z0-9_]*)`)

// RenderProject renders the deterministic dbt project for one published DAG
// version.
//
// The target catalog defaults to DefaultCatalog when catalog is empty. The
// target schema is derived from the tenant and Pipeline ids as
// oa_t<tenant8>_p<pipeline8> (design D9), so it is stable across versions. A
// DAG without tasks still yields a valid project with dbt_project.yml and
// profiles.yml only.
func RenderProject(tenantID, pipelineID string, version int, dag domain.DAG, catalog string) (Project, error) {
	targetCatalog := strings.TrimSpace(catalog)
	if targetCatalog == "" {
		targetCatalog = DefaultCatalog
	}
	if !catalogPattern.MatchString(targetCatalog) {
		return Project{}, &RenderError{
			Code:    CodeInvalidTargetCatalog,
			Message: fmt.Sprintf("target catalog %q is not a valid identifier", targetCatalog),
		}
	}
	if version < 1 {
		return Project{}, &RenderError{
			Code:    CodeInvalidVersion,
			Message: fmt.Sprintf("published version %d must be at least 1", version),
		}
	}
	schema, err := DeriveSchema(tenantID, pipelineID)
	if err != nil {
		return Project{}, err
	}

	files := []File{
		{Path: "dbt_project.yml", Content: []byte(dbtProjectFile)},
		{Path: "profiles.yml", Content: []byte(fmt.Sprintf(profilesFileTemplate, targetCatalog, schema))},
	}

	seenTaskIDs := make(map[string]struct{}, len(dag.Tasks))
	models := make([]File, 0, len(dag.Tasks))
	for _, task := range dag.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if !validModelFileName(taskID) {
			return Project{}, &RenderError{
				Code:    CodeInvalidTaskID,
				Message: fmt.Sprintf("task id %q cannot be used as a dbt model file name", task.ID),
			}
		}
		if _, ok := seenTaskIDs[taskID]; ok {
			return Project{}, &RenderError{
				Code:    CodeDuplicateTaskID,
				Message: fmt.Sprintf("duplicate task id %q", taskID),
			}
		}
		seenTaskIDs[taskID] = struct{}{}
		if strings.TrimSpace(task.Type) != domain.TaskTypeDBT {
			return Project{}, &RenderError{
				Code:    CodeUnsupportedTaskType,
				Message: fmt.Sprintf("task %q has unsupported type %q (want %q)", taskID, task.Type, domain.TaskTypeDBT),
			}
		}
		if err := checkSQLReferences(taskID, task.SQL, targetCatalog, schema); err != nil {
			return Project{}, err
		}
		models = append(models, File{Path: "models/" + taskID + ".sql", Content: []byte(task.SQL)})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })
	files = append(files, models...)

	return Project{
		Files:       files,
		Schema:      schema,
		ContentHash: contentHash(files),
	}, nil
}

// DeriveSchema returns the tenant/Pipeline scoped dbt schema
// oa_t<tenant8>_p<pipeline8>. Both ids must be UUIDs so the derived schema is
// stable and collision resistant; output writeback uses the same derivation.
func DeriveSchema(tenantID, pipelineID string) (string, error) {
	tenant, ok := uuIDSegment(tenantID)
	if !ok {
		return "", &RenderError{Code: CodeInvalidTenantID, Message: "tenant id must be a UUID"}
	}
	pipeline, ok := uuIDSegment(pipelineID)
	if !ok {
		return "", &RenderError{Code: CodeInvalidPipelineID, Message: "pipeline id must be a UUID"}
	}
	return "oa_t" + tenant + "_p" + pipeline, nil
}

// uuIDSegment normalises a UUID and returns its first eight lowercase hex
// characters (the "tenant8"/"pipeline8" fragment of the derived schema).
func uuIDSegment(raw string) (string, bool) {
	compact := strings.ReplaceAll(strings.TrimSpace(raw), "-", "")
	if len(compact) != 32 {
		return "", false
	}
	for i := 0; i < len(compact); i++ {
		switch char := compact[i]; {
		case char >= '0' && char <= '9':
		case char >= 'a' && char <= 'f':
		case char >= 'A' && char <= 'F':
		default:
			return "", false
		}
	}
	return strings.ToLower(compact[:8]), true
}

// validModelFileName rejects ids that would escape the models directory or
// produce an unusable dbt model path. Published DAG ids are simple slugs.
func validModelFileName(taskID string) bool {
	if taskID == "" || taskID == "." || taskID == ".." {
		return false
	}
	if strings.ContainsAny(taskID, "/\\") || strings.Contains(taskID, "..") {
		return false
	}
	for _, char := range taskID {
		if char == 0 || char == '\n' || char == '\r' {
			return false
		}
	}
	return true
}

// checkSQLReferences rejects explicit three-part references whose catalog or
// schema is outside the rendered target. It is defense in depth, not a security
// boundary: comments, single-quoted literals and dynamic SQL are skipped and
// fail at runtime instead (design D9).
func checkSQLReferences(taskID, sql, targetCatalog, targetSchema string) error {
	cleaned := normaliseSQLForReferenceScan(sql)
	for _, match := range qualifiedRelationPattern.FindAllStringSubmatch(cleaned, -1) {
		relationCatalog, relationSchema, relationTable := match[1], match[2], match[3]
		if strings.EqualFold(relationCatalog, targetCatalog) && strings.EqualFold(relationSchema, targetSchema) {
			continue
		}
		return &RenderError{
			Code: CodeInvalidSQLReference,
			Message: fmt.Sprintf(
				"task %q references %s.%s.%s outside controlled catalog %q and schema %q",
				taskID, relationCatalog, relationSchema, relationTable, targetCatalog, targetSchema,
			),
		}
	}
	return nil
}

// normaliseSQLForReferenceScan blanks comments and single-quoted string
// literals, then removes double quotes so quoted identifiers participate in the
// three-part scan. It is intentionally not a SQL parser.
func normaliseSQLForReferenceScan(sql string) string {
	var cleaned strings.Builder
	cleaned.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				cleaned.WriteByte(' ')
				i++
			}
		case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
			cleaned.WriteString("  ")
			i += 2
			for i < len(sql) {
				if sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/' {
					cleaned.WriteString("  ")
					i += 2
					break
				}
				if sql[i] == '\n' {
					cleaned.WriteByte('\n')
				} else {
					cleaned.WriteByte(' ')
				}
				i++
			}
		case sql[i] == '\'':
			cleaned.WriteByte(' ')
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						cleaned.WriteString("  ")
						i += 2
						continue
					}
					cleaned.WriteByte(' ')
					i++
					break
				}
				if sql[i] == '\n' {
					cleaned.WriteByte('\n')
				} else {
					cleaned.WriteByte(' ')
				}
				i++
			}
		default:
			cleaned.WriteByte(sql[i])
			i++
		}
	}
	return strings.ReplaceAll(cleaned.String(), `"`, "")
}

// contentHash returns sha256:<hex> over the sorted path/content pairs. Each
// pair is hashed as path, NUL, content, NUL so concatenation cannot collide.
func contentHash(files []File) string {
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	hasher := sha256.New()
	for _, file := range sorted {
		hasher.Write([]byte(file.Path))
		hasher.Write([]byte{0})
		hasher.Write(file.Content)
		hasher.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

// ErrInvalidRunResults indicates that a dbt run_results.json artifact is
// missing, unreadable or structurally invalid. Errors wrapping it never echo
// SQL, credentials or raw artifact bytes.
var ErrInvalidRunResults = errors.New("invalid dbt run_results.json")

// RunResult is one model-level dbt result. Message is the engine-provided
// reason and is kept for telemetry; callers must sanitise it before exposing it
// to users.
type RunResult struct {
	UniqueID string
	Status   string
	Message  string
}

// RunSummary is the minimal reconciliation view of a run_results.json
// artifact. Statuses are counted fail-closed: only explicit success/pass count
// as Success, while an unrecognised status counts as Failed so it can never be
// reported as success (design D7).
type RunSummary struct {
	Total   int
	Success int
	Failed  int
	Skipped int
	Results []RunResult
}

// ParseRunResults parses the minimal dbt run_results.json structure
// {results:[{unique_id,status,message}]}. A missing/null results array or an
// entry without unique_id/status is an error.
func ParseRunResults(raw []byte) (RunSummary, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return RunSummary{}, fmt.Errorf("%w: artifact is empty", ErrInvalidRunResults)
	}
	var artifact struct {
		Results *[]struct {
			UniqueID string `json:"unique_id"`
			Status   string `json:"status"`
			Message  string `json:"message"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(trimmed), &artifact); err != nil {
		return RunSummary{}, fmt.Errorf("%w: artifact is not valid JSON", ErrInvalidRunResults)
	}
	if artifact.Results == nil {
		return RunSummary{}, fmt.Errorf("%w: results array is missing or null", ErrInvalidRunResults)
	}

	summary := RunSummary{Results: make([]RunResult, 0, len(*artifact.Results))}
	for index, node := range *artifact.Results {
		uniqueID := strings.TrimSpace(node.UniqueID)
		status := strings.TrimSpace(node.Status)
		if uniqueID == "" {
			return RunSummary{}, fmt.Errorf("%w: results[%d].unique_id is empty", ErrInvalidRunResults, index)
		}
		if status == "" {
			return RunSummary{}, fmt.Errorf("%w: results[%d].status is empty", ErrInvalidRunResults, index)
		}
		summary.Total++
		switch strings.ToLower(status) {
		case "success", "pass":
			summary.Success++
		case "skipped":
			summary.Skipped++
		default:
			// "error", "fail", "failed", "runtime error" and any unknown status
			// fail closed (design D7: unknown engine state is never success).
			summary.Failed++
		}
		summary.Results = append(summary.Results, RunResult{
			UniqueID: uniqueID,
			Status:   status,
			Message:  node.Message,
		})
	}
	return summary, nil
}
