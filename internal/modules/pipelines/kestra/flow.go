// Package kestra renders the engine-neutral Flow IR and the Kestra Flow YAML
// for a published Pipeline version. Rendering is pure and deterministic: the
// same IR always produces byte-identical YAML, and BuildFlowIR is the only
// place where a validated authoring DAG is mapped onto flow tasks.
package kestra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

const (
	// DefaultEnvironment is used when the deployment does not name one. It is
	// a label value only, never a credential source.
	DefaultEnvironment = "local"

	// wrapperTaskID is the id of the Sequential task that wraps every
	// rendered pipeline task. Sequential is the version-independent fallback
	// while io.kestra.plugin.core.flow.Dag availability is unpinned (design
	// D3); execution order is the deterministic topological order.
	wrapperTaskID = "ontoaxis_pipeline"
	// noopTaskID is the single Log task rendered for a DAG without tasks.
	noopTaskID = "noop"

	flowTaskTypeSequential    = "io.kestra.plugin.core.flow.Sequential"
	flowTaskTypeLog           = "io.kestra.plugin.core.log.Log"
	flowTaskTypeShellCommands = "io.kestra.plugin.scripts.shell.Commands"
)

// FlowIR is the execution-engine-neutral representation of one published
// Pipeline version. It contains no credentials and no SQL text: dbt SQL lives
// in the namespace project files and is referenced by model name.
type FlowIR struct {
	Namespace string
	FlowID    string
	Revision  int
	Labels    map[string]string
	Tasks     []FlowTask
	NoOp      bool
}

// FlowTask is one dbt model execution inside a FlowIR, in topological order.
type FlowTask struct {
	ID           string
	DependsOn    []string
	Model        string
	SQL          string
	Materialized string
}

// BuildFlowIR derives the deterministic Flow IR for one Pipeline version.
// The namespace is oa_<tenant_hex32>.<pipeline_hex32> and the flow id is the
// pipeline hex32, both derived from UUIDs so tenants cannot collide. Labels
// carry only identity/version metadata and never credentials (design D4).
func BuildFlowIR(tenantID, pipelineID string, version int, dag domain.DAG, environment string) (FlowIR, error) {
	tenantLabel, tenantHex, err := parseUUID(tenantID, "tenant id")
	if err != nil {
		return FlowIR{}, err
	}
	pipelineLabel, pipelineHex, err := parseUUID(pipelineID, "pipeline id")
	if err != nil {
		return FlowIR{}, err
	}

	env := strings.TrimSpace(environment)
	if env == "" {
		env = DefaultEnvironment
	}

	ordered, err := orderTasks(dag.Tasks)
	if err != nil {
		return FlowIR{}, err
	}

	ir := FlowIR{
		Namespace: namespacePrefix + "_" + tenantHex + "." + pipelineHex,
		FlowID:    pipelineHex,
		Revision:  version,
		Labels: map[string]string{
			labelTenantID:        tenantLabel,
			labelPipelineID:      pipelineLabel,
			labelPipelineVersion: strconv.Itoa(version),
			labelDAGVersion:      strconv.Itoa(dag.DAGVersion),
			labelManagedBy:       managedByValue,
			labelEnvironment:     env,
		},
		NoOp: len(ordered) == 0,
	}
	if ir.NoOp {
		return ir, nil
	}

	tasks := make([]FlowTask, 0, len(ordered))
	for _, task := range ordered {
		tasks = append(tasks, FlowTask{
			ID:           strings.TrimSpace(task.ID),
			DependsOn:    normalizeDependencies(task.DependsOn),
			Model:        task.Model,
			SQL:          task.SQL,
			Materialized: task.Materialized,
		})
	}
	ir.Tasks = tasks
	return ir, nil
}

// RenderFlowYAML renders the Kestra Flow definition for ir. Labels are emitted
// in sorted key order, tasks in IR (topological) order, and the output is
// byte-identical for the same IR. A DAG without tasks renders a Sequential
// wrapper containing one Log task (Stage 1 compatibility, design D2/D3).
func RenderFlowYAML(ir FlowIR) ([]byte, error) {
	if strings.TrimSpace(ir.FlowID) == "" || strings.TrimSpace(ir.Namespace) == "" {
		return nil, fmt.Errorf("%w: flow id and namespace are required to render a flow", domain.ErrInvalidArgument)
	}
	tasks, err := renderTasks(ir)
	if err != nil {
		return nil, err
	}
	document := flowDocument{
		ID:        ir.FlowID,
		Namespace: ir.Namespace,
		Revision:  ir.Revision,
		Labels:    copyLabels(ir.Labels),
		Tasks:     tasks,
	}

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("kestra: encode flow yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("kestra: close flow yaml encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

// FlowHash is the content hash recorded on Flow registration. It is stable for
// identical rendered bytes.
func FlowHash(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// flowDocument is the root of the rendered Kestra Flow YAML. Field order is
// the documented root key order; the labels map is sorted by the YAML marshaller.
type flowDocument struct {
	ID        string             `yaml:"id"`
	Namespace string             `yaml:"namespace"`
	Revision  int                `yaml:"revision"`
	Labels    map[string]string  `yaml:"labels"`
	Tasks     []flowTaskDocument `yaml:"tasks"`
}

// flowTaskDocument is one rendered Kestra task. Commands, dependsOn, message
// and tasks are omitted when empty so a dbt task and a no-op log task stay
// minimal.
// flowTaskRunner selects the in-container runner. The Kestra image already
// contains dbt-trino, so scripts run through the Process runner instead of
// requiring a Docker socket.
type flowTaskRunner struct {
	Type string `yaml:"type"`
}

type flowNamespaceFiles struct {
	Enabled bool `yaml:"enabled"`
}

type flowTaskDocument struct {
	ID             string              `yaml:"id"`
	Type           string              `yaml:"type"`
	TaskRunner     *flowTaskRunner     `yaml:"taskRunner,omitempty"`
	NamespaceFiles *flowNamespaceFiles `yaml:"namespaceFiles,omitempty"`
	OutputFiles    []string            `yaml:"outputFiles,omitempty"`
	Commands       []string            `yaml:"commands,omitempty"`
	DependsOn      []string            `yaml:"dependsOn,omitempty"`
	Message        string              `yaml:"message,omitempty"`
	Tasks          []flowTaskDocument  `yaml:"tasks,omitempty"`
}

func renderTasks(ir FlowIR) ([]flowTaskDocument, error) {
	sequential := flowTaskDocument{ID: wrapperTaskID, Type: flowTaskTypeSequential}
	if ir.NoOp || len(ir.Tasks) == 0 {
		sequential.Tasks = []flowTaskDocument{{
			ID:      noopTaskID,
			Type:    flowTaskTypeLog,
			Message: "pipeline has no tasks",
		}}
		return []flowTaskDocument{sequential}, nil
	}

	children := make([]flowTaskDocument, 0, len(ir.Tasks))
	for _, task := range ir.Tasks {
		if strings.TrimSpace(task.ID) == "" {
			return nil, fmt.Errorf("%w: flow task id must not be empty", domain.ErrInvalidArgument)
		}
		if strings.TrimSpace(task.Model) == "" {
			return nil, fmt.Errorf(
				"%w: flow task %q has no model to select", domain.ErrInvalidArgument, task.ID,
			)
		}
		model := strings.TrimSpace(task.Model)
		children = append(children, flowTaskDocument{
			ID:             strings.TrimSpace(task.ID),
			Type:           flowTaskTypeShellCommands,
			TaskRunner:     &flowTaskRunner{Type: "io.kestra.plugin.core.runner.Process"},
			NamespaceFiles: &flowNamespaceFiles{Enabled: true},
			OutputFiles:    []string{"target/run_results*.json"},
			Commands: []string{
				fmt.Sprintf("dbt run --select %s --project-dir . --profiles-dir .", model),
				"cp target/run_results.json target/run_results.run.json",
				fmt.Sprintf("dbt test --select %s --project-dir . --profiles-dir .", model),
				"cp target/run_results.json target/run_results.test.json",
			},
			DependsOn: append([]string(nil), task.DependsOn...),
		})
	}
	sequential.Tasks = children
	return []flowTaskDocument{sequential}, nil
}

// orderTasks returns dag.Tasks in a deterministic topological order: ready
// tasks are emitted by ascending task id, so independent tasks have a stable
// tie-break. A validate-first caller never sees the returned errors, but the
// renderer refuses to invent an order for an invalid graph.
func orderTasks(tasks []domain.Task) ([]domain.Task, error) {
	byID := make(map[string]domain.Task, len(tasks))
	indegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		id := strings.TrimSpace(task.ID)
		if id == "" {
			return nil, &domain.ValidationError{
				Code:    domain.CodeDuplicateTaskID,
				Message: "task id must not be empty",
			}
		}
		if _, exists := byID[id]; exists {
			return nil, &domain.ValidationError{
				Code:    domain.CodeDuplicateTaskID,
				Message: fmt.Sprintf("duplicate task id %q", id),
			}
		}
		byID[id] = task
		indegree[id] = 0
	}

	for _, task := range tasks {
		id := strings.TrimSpace(task.ID)
		seen := make(map[string]struct{}, len(task.DependsOn))
		for _, dependency := range task.DependsOn {
			reference := strings.TrimSpace(dependency)
			if _, exists := byID[reference]; !exists {
				return nil, &domain.ValidationError{
					Code:    domain.CodeMissingDependency,
					Message: fmt.Sprintf("task %q depends on unknown task %q", id, reference),
				}
			}
			if _, duplicate := seen[reference]; duplicate {
				continue
			}
			seen[reference] = struct{}{}
			indegree[id]++
			dependents[reference] = append(dependents[reference], id)
		}
	}

	ready := make([]string, 0, len(tasks))
	for id, count := range indegree {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ordered := make([]domain.Task, 0, len(tasks))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[id])
		children := append([]string(nil), dependents[id]...)
		sort.Strings(children)
		for _, child := range children {
			indegree[child]--
			if indegree[child] == 0 {
				ready = insertSorted(ready, child)
			}
		}
	}
	if len(ordered) != len(byID) {
		blocked := make([]string, 0, len(byID)-len(ordered))
		for id, count := range indegree {
			if count > 0 {
				blocked = append(blocked, id)
			}
		}
		sort.Strings(blocked)
		return nil, &domain.ValidationError{
			Code:    domain.CodeDependencyCycle,
			Message: fmt.Sprintf("dependency cycle involving task %q", blocked[0]),
		}
	}
	return ordered, nil
}

// normalizeDependencies trims, de-duplicates and sorts a dependency list so the
// rendered YAML does not depend on the written order of depends_on.
func normalizeDependencies(dependencies []string) []string {
	if len(dependencies) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(dependencies))
	normalized := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		reference := strings.TrimSpace(dependency)
		if reference == "" {
			continue
		}
		if _, duplicate := seen[reference]; duplicate {
			continue
		}
		seen[reference] = struct{}{}
		normalized = append(normalized, reference)
	}
	sort.Strings(normalized)
	return normalized
}

func copyLabels(labels map[string]string) map[string]string {
	copied := make(map[string]string, len(labels))
	for key, value := range labels {
		copied[key] = value
	}
	return copied
}

// parseUUID returns the canonical dashed lowercase UUID string and its 32
// lowercase hex form without dashes.
func parseUUID(raw, field string) (canonical string, hex32 string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("%w: %s is required", domain.ErrInvalidArgument, field)
	}
	parsed, parseErr := uuid.Parse(trimmed)
	if parseErr != nil {
		return "", "", fmt.Errorf("%w: %s must be a UUID", domain.ErrInvalidArgument, field)
	}
	return parsed.String(), hex.EncodeToString(parsed[:]), nil
}

func insertSorted(sortedIDs []string, id string) []string {
	index := sort.SearchStrings(sortedIDs, id)
	sortedIDs = append(sortedIDs, "")
	copy(sortedIDs[index+1:], sortedIDs[index:])
	sortedIDs[index] = id
	return sortedIDs
}
