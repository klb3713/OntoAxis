package dbt_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/dbt"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

const (
	testTenantID   = "11111111-2222-3333-4444-555555555555"
	testPipelineID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testSchema     = "oa_t11111111_paaaaaaaa"
	taskA          = "task_a"
	taskB          = "task_b"
	sqlA           = "select 1 as id\n"
	sqlB           = "select 2 as id\n"
)

func dbtTask(id, model, sql string) domain.Task {
	return domain.Task{ID: id, Type: domain.TaskTypeDBT, Model: model, SQL: sql}
}

func renderDAG(tasks ...domain.Task) domain.DAG {
	return domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: tasks}
}

func fileByPath(t *testing.T, project dbt.Project, path string) dbt.File {
	t.Helper()
	for _, file := range project.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("rendered project has no file %q (files: %v)", path, filePaths(project))
	return dbt.File{}
}

func filePaths(project dbt.Project) []string {
	paths := make([]string, 0, len(project.Files))
	for _, file := range project.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

// expectedContentHash re-derives the documented hash contract independently of
// the production helper: sha256 over sorted path+NUL+content+NUL pairs.
func expectedContentHash(files []dbt.File) string {
	sorted := append([]dbt.File(nil), files...)
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

const expectedProjectFile = `name: ontoaxis_pipeline
version: "1.0.0"
config-version: 2
profile: ontoaxis_pipeline
model-paths: ["models"]
models:
  ontoaxis_pipeline:
    +materialized: table
`

const expectedProfilesFile = `ontoaxis_pipeline:
  target: prod
  outputs:
    prod:
      type: trino
      method: none
      host: "{{ env_var('DBT_TRINO_HOST') }}"
      port: "{{ env_var('DBT_TRINO_PORT') }}"
      user: "{{ env_var('DBT_TRINO_USER') }}"
      database: "memory"
      schema: "oa_t11111111_paaaaaaaa"
      threads: 2
`

func TestRenderProjectGolden(t *testing.T) {
	project, err := dbt.RenderProject(
		testTenantID,
		testPipelineID,
		3,
		// Declaration order is deliberately not sorted: files must be sorted by
		// path so the project is byte-for-byte deterministic.
		renderDAG(dbtTask(taskB, "model_b", sqlB), dbtTask(taskA, "model_a", sqlA)),
		"",
	)
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}

	expected := dbt.Project{
		Files: []dbt.File{
			{Path: "dbt_project.yml", Content: []byte(expectedProjectFile)},
			{Path: "profiles.yml", Content: []byte(expectedProfilesFile)},
			{Path: "models/task_a.sql", Content: []byte(sqlA)},
			{Path: "models/task_b.sql", Content: []byte(sqlB)},
		},
		Schema: testSchema,
	}
	expected.ContentHash = expectedContentHash(expected.Files)
	if !reflect.DeepEqual(project, expected) {
		t.Fatalf("rendered project mismatch\ngot:  %#v\nwant: %#v", project, expected)
	}
	if !strings.HasPrefix(project.ContentHash, "sha256:") || len(project.ContentHash) != len("sha256:")+64 {
		t.Fatalf("unexpected content hash %q", project.ContentHash)
	}
}

func TestRenderProjectIsDeterministic(t *testing.T) {
	first, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
		renderDAG(dbtTask(taskA, "model_a", sqlA), dbtTask(taskB, "model_b", sqlB)), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	second, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
		renderDAG(dbtTask(taskA, "model_a", sqlA), dbtTask(taskB, "model_b", sqlB)), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("rendering the same DAG twice must produce identical bytes")
	}

	// Task declaration order is not part of the rendered content.
	reordered, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
		renderDAG(dbtTask(taskB, "model_b", sqlB), dbtTask(taskA, "model_a", sqlA)), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if !reflect.DeepEqual(first, reordered) {
		t.Fatal("task declaration order must not change the rendered project")
	}

	changed, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
		renderDAG(dbtTask(taskA, "model_a", "select 3 as id\n"), dbtTask(taskB, "model_b", sqlB)), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if changed.ContentHash == first.ContentHash {
		t.Fatal("content hash must change when model SQL changes")
	}
}

func TestRenderProjectSchemaIsStableAcrossVersions(t *testing.T) {
	version1, err := dbt.RenderProject(testTenantID, testPipelineID, 1, renderDAG(), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	version2, err := dbt.RenderProject(testTenantID, testPipelineID, 2, renderDAG(), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if version1.Schema != testSchema || version2.Schema != testSchema {
		t.Fatalf("schema must be stable across versions, got %q and %q", version1.Schema, version2.Schema)
	}
	profiles := string(fileByPath(t, version1, "profiles.yml").Content)
	if !strings.Contains(profiles, `schema: "`+testSchema+`"`) {
		t.Fatalf("profiles.yml must pin the derived schema, got:\n%s", profiles)
	}
}

func TestRenderProjectNoOpDAG(t *testing.T) {
	project, err := dbt.RenderProject(testTenantID, testPipelineID, 1, domain.DAG{}, "")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if project.Schema != testSchema {
		t.Fatalf("schema = %q, want %q", project.Schema, testSchema)
	}
	if got, want := filePaths(project), []string{"dbt_project.yml", "profiles.yml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-op DAG files = %v, want %v", got, want)
	}
	if project.ContentHash != expectedContentHash(project.Files) {
		t.Fatal("no-op DAG content hash does not match the documented contract")
	}
}

func TestRenderProjectCatalogSelection(t *testing.T) {
	project, err := dbt.RenderProject(testTenantID, testPipelineID, 1, renderDAG(), "")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if !strings.Contains(string(fileByPath(t, project, "profiles.yml").Content), `database: "memory"`) {
		t.Fatal("empty catalog must default to the local memory catalog")
	}

	project, err = dbt.RenderProject(testTenantID, testPipelineID, 1, renderDAG(), "hive")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	if !strings.Contains(string(fileByPath(t, project, "profiles.yml").Content), `database: "hive"`) {
		t.Fatal("explicit catalog must be rendered into profiles.yml")
	}
}

func TestRenderProjectHasNoCredentials(t *testing.T) {
	project, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
		renderDAG(dbtTask(taskA, "model_a", "select 1 as id\n")), "memory")
	if err != nil {
		t.Fatalf("RenderProject: %v", err)
	}
	var rendered strings.Builder
	for _, file := range project.Files {
		rendered.Write(file.Content)
		rendered.WriteByte('\n')
	}
	joined := rendered.String()
	for _, forbidden := range []string{
		"password", "PASSWORD", "secret", "SECRET", "token", "TOKEN",
		"http://", "https://", "Authorization", "admin", "dsn:", "DSN",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("rendered project contains literal %q; connection values must be env_var references", forbidden)
		}
	}
	profiles := string(fileByPath(t, project, "profiles.yml").Content)
	for _, reference := range []string{
		`host: "{{ env_var('DBT_TRINO_HOST') }}"`,
		`port: "{{ env_var('DBT_TRINO_PORT') }}"`,
		`user: "{{ env_var('DBT_TRINO_USER') }}"`,
		"threads: 2",
	} {
		if !strings.Contains(profiles, reference) {
			t.Errorf("profiles.yml missing %q, got:\n%s", reference, profiles)
		}
	}
}

func TestRenderProjectRejectsOutOfScopeSQLReferences(t *testing.T) {
	allowedRelation := "memory." + testSchema + ".orders"
	tests := []struct {
		name       string
		catalog    string
		sql        string
		wantCode   string
		wantReject bool
	}{
		{
			name:    "controlled catalog and schema is allowed",
			catalog: "memory",
			sql:     "select * from " + allowedRelation + "\n",
		},
		{
			name:    "explicit catalog with controlled schema is allowed",
			catalog: "hive",
			sql:     "select * from hive." + testSchema + ".orders\n",
		},
		{
			name:       "foreign catalog is rejected",
			catalog:    "memory",
			sql:        "select * from hive." + testSchema + ".orders\n",
			wantCode:   dbt.CodeInvalidSQLReference,
			wantReject: true,
		},
		{
			name:       "foreign schema is rejected",
			catalog:    "memory",
			sql:        "select * from memory.other_schema.orders\n",
			wantCode:   dbt.CodeInvalidSQLReference,
			wantReject: true,
		},
		{
			name:       "quoted identifiers are rejected too",
			catalog:    "memory",
			sql:        "select * from \"hive\".\"other_schema\".\"orders\"\n",
			wantCode:   dbt.CodeInvalidSQLReference,
			wantReject: true,
		},
		{
			name:       "lowercase and uppercase catalog match case-insensitively",
			catalog:    "memory",
			sql:        "select * from MEMORY." + strings.ToUpper(testSchema) + ".orders\n",
			wantCode:   "",
			wantReject: false,
		},
		{
			name:    "references inside string literals are ignored",
			catalog: "memory",
			sql:     "select 'hive.other_schema.orders' as marker\n",
		},
		{
			name:    "references inside line comments are ignored",
			catalog: "memory",
			sql:     "select 1 -- hive.other_schema.orders\n",
		},
		{
			name:    "references inside block comments are ignored",
			catalog: "memory",
			sql:     "select 1 /* hive.other_schema.orders */\n",
		},
		{
			name:    "dynamic SQL is skipped and left to runtime failure",
			catalog: "memory",
			sql:     "execute immediate 'select * from hive.other_schema.orders'\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dbt.RenderProject(testTenantID, testPipelineID, 1,
				renderDAG(dbtTask(taskA, "model_a", test.sql)), test.catalog)
			if !test.wantReject {
				if err != nil {
					t.Fatalf("RenderProject: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected invalid_sql_reference error")
			}
			if code := dbt.ErrorCode(err); code != test.wantCode {
				t.Fatalf("error code = %q, want %q (err: %v)", code, test.wantCode, err)
			}
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("rendering error must unwrap to ErrInvalidArgument, got %v", err)
			}
			if !strings.Contains(err.Error(), dbt.CodeInvalidSQLReference) {
				t.Fatalf("error must mention %q, got %v", dbt.CodeInvalidSQLReference, err)
			}
			if strings.Contains(err.Error(), "select *") {
				t.Fatalf("error must not echo full SQL, got %v", err)
			}
		})
	}
}

func TestRenderProjectRejectsInvalidIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		pipelineID string
		version    int
		catalog    string
		wantCode   string
	}{
		{name: "empty tenant", tenantID: "", pipelineID: testPipelineID, version: 1, catalog: "memory", wantCode: dbt.CodeInvalidTenantID},
		{name: "non-uuid tenant", tenantID: "tenant-a", pipelineID: testPipelineID, version: 1, catalog: "memory", wantCode: dbt.CodeInvalidTenantID},
		{name: "short uuid tenant", tenantID: "11111111", pipelineID: testPipelineID, version: 1, catalog: "memory", wantCode: dbt.CodeInvalidTenantID},
		{name: "non-uuid pipeline", tenantID: testTenantID, pipelineID: "pipeline-a", version: 1, catalog: "memory", wantCode: dbt.CodeInvalidPipelineID},
		{name: "non-identifier catalog", tenantID: testTenantID, pipelineID: testPipelineID, version: 1, catalog: "bad-catalog!", wantCode: dbt.CodeInvalidTargetCatalog},
		{name: "draft version", tenantID: testTenantID, pipelineID: testPipelineID, version: 0, catalog: "memory", wantCode: dbt.CodeInvalidVersion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dbt.RenderProject(test.tenantID, test.pipelineID, test.version, renderDAG(), test.catalog)
			if err == nil {
				t.Fatal("expected rendering error")
			}
			if code := dbt.ErrorCode(err); code != test.wantCode {
				t.Fatalf("error code = %q, want %q (err: %v)", code, test.wantCode, err)
			}
		})
	}
}

func TestRenderProjectRejectsInvalidTasks(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []domain.Task
		wantCode string
	}{
		{
			name:     "unsupported task type",
			tasks:    []domain.Task{{ID: taskA, Type: "python", Model: "model_a", SQL: sqlA}},
			wantCode: dbt.CodeUnsupportedTaskType,
		},
		{
			name:     "path traversal in task id",
			tasks:    []domain.Task{dbtTask("../escape", "model_a", sqlA)},
			wantCode: dbt.CodeInvalidTaskID,
		},
		{
			name:     "duplicate task id",
			tasks:    []domain.Task{dbtTask(taskA, "model_a", sqlA), dbtTask(taskA, "model_b", sqlB)},
			wantCode: dbt.CodeDuplicateTaskID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dbt.RenderProject(testTenantID, testPipelineID, 1, renderDAG(test.tasks...), "memory")
			if err == nil {
				t.Fatal("expected rendering error")
			}
			if code := dbt.ErrorCode(err); code != test.wantCode {
				t.Fatalf("error code = %q, want %q (err: %v)", code, test.wantCode, err)
			}
		})
	}
}

func TestDeriveSchema(t *testing.T) {
	schema, err := dbt.DeriveSchema(testTenantID, testPipelineID)
	if err != nil {
		t.Fatalf("DeriveSchema: %v", err)
	}
	if schema != testSchema {
		t.Fatalf("DeriveSchema = %q, want %q", schema, testSchema)
	}

	upper, err := dbt.DeriveSchema(strings.ToUpper(testTenantID), strings.ToUpper(testPipelineID))
	if err != nil {
		t.Fatalf("DeriveSchema uppercase: %v", err)
	}
	if upper != testSchema {
		t.Fatalf("uppercase UUIDs must derive the same schema, got %q", upper)
	}

	if _, err := dbt.DeriveSchema(testTenantID, "not-a-uuid"); dbt.ErrorCode(err) != dbt.CodeInvalidPipelineID {
		t.Fatalf("invalid pipeline id code = %q, err = %v", dbt.ErrorCode(err), err)
	}
}

func TestParseRunResults(t *testing.T) {
	raw := []byte(`{
		"metadata": {"dbt_version": "1.8.0"},
		"results": [
			{"unique_id": "model.pipeline.task_a", "status": "success", "message": null},
			{"unique_id": "model.pipeline.task_b", "status": "error", "message": "compilation error"},
			{"unique_id": "test.pipeline.not_null_task_a", "status": "skipped", "message": ""},
			{"unique_id": "test.pipeline.unique_task_a", "status": "pass", "message": "ok"}
		]
	}`)
	summary, err := dbt.ParseRunResults(raw)
	if err != nil {
		t.Fatalf("ParseRunResults: %v", err)
	}
	if summary.Total != 4 || summary.Success != 2 || summary.Failed != 1 || summary.Skipped != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(summary.Results) != 4 {
		t.Fatalf("model-level results = %d, want 4", len(summary.Results))
	}
	if summary.Results[1].UniqueID != "model.pipeline.task_b" || summary.Results[1].Message != "compilation error" {
		t.Fatalf("unexpected model result: %+v", summary.Results[1])
	}
}

func TestParseRunResultsEmptyResults(t *testing.T) {
	summary, err := dbt.ParseRunResults([]byte(`{"results": []}`))
	if err != nil {
		t.Fatalf("ParseRunResults: %v", err)
	}
	if summary.Total != 0 || summary.Success != 0 || summary.Failed != 0 || summary.Skipped != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestParseRunResultsUnknownStatusFailsClosed(t *testing.T) {
	summary, err := dbt.ParseRunResults([]byte(`{"results": [
		{"unique_id": "model.pipeline.task_a", "status": "success"},
		{"unique_id": "test.pipeline.warn", "status": "warn"},
		{"unique_id": "test.pipeline.runtime", "status": "runtime error"}
	]}`))
	if err != nil {
		t.Fatalf("ParseRunResults: %v", err)
	}
	if summary.Success != 1 || summary.Failed != 2 || summary.Skipped != 0 {
		t.Fatalf("unknown statuses must fail closed, got %+v", summary)
	}
}

func TestParseRunResultsRejectsInvalidArtifacts(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "nil", raw: nil},
		{name: "empty", raw: []byte("")},
		{name: "whitespace", raw: []byte("  \n\t ")},
		{name: "not json", raw: []byte("dbt failed: password=hunter2")},
		{name: "top-level array", raw: []byte(`[{"unique_id": "model.a", "status": "success"}]`)},
		{name: "missing results", raw: []byte(`{"metadata": {}}`)},
		{name: "null results", raw: []byte(`{"results": null}`)},
		{name: "non-array results", raw: []byte(`{"results": "nope"}`)},
		{name: "missing unique id", raw: []byte(`{"results": [{"status": "success"}]}`)},
		{name: "missing status", raw: []byte(`{"results": [{"unique_id": "model.a"}]}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dbt.ParseRunResults(test.raw)
			if err == nil {
				t.Fatal("expected invalid run_results error")
			}
			if !errors.Is(err, dbt.ErrInvalidRunResults) {
				t.Fatalf("error must wrap ErrInvalidRunResults, got %v", err)
			}
			if dbt.ErrorCode(err) != "" {
				t.Fatalf("parse errors must not be render errors, got code %q", dbt.ErrorCode(err))
			}
			for _, leaked := range []string{"hunter2", "password", "select *"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("error leaked artifact content %q: %v", leaked, err)
				}
			}
		})
	}
}
