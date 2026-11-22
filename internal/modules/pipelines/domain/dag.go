package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DAGVersionV1 is the only authoring DAG version understood by this change.
// Definitions that omit dag_version are interpreted as v1 (Stage 1
// compatibility, see design D2).
const DAGVersionV1 = 1

// TaskTypeDBT is the only supported authoring task type in DAG v1. The type
// set is intentionally closed: unknown types are rejected at publish time so a
// definition cannot be silently skipped by a renderer that does not know it.
const TaskTypeDBT = "dbt"

// Stable validation codes. They are part of the publish error contract: the
// API maps every ValidationError to 400 invalid_argument and callers must be
// able to branch on Code, never on Message.
const (
	CodeInvalidDAGVersion   = "invalid_dag_version"
	CodeDuplicateTaskID     = "duplicate_task_id"
	CodeDependencyCycle     = "dependency_cycle"
	CodeUnknownTaskType     = "unknown_task_type"
	CodeMissingDependency   = "missing_dependency"
	CodeInvalidSQLReference = "invalid_sql_reference"
)

// DAG is the parsed authoring DAG v1 document. JSON field names are the wire
// names; ParseDAG is the only supported entry point because it applies the
// version default and lenient unknown-field handling.
type DAG struct {
	DAGVersion int      `json:"dag_version"`
	DatasetIDs []string `json:"dataset_ids"`
	Inputs     []Input  `json:"inputs"`
	Tasks      []Task   `json:"tasks"`
	Outputs    []Output `json:"outputs"`
}

// Input is one referenced input dataset.
type Input struct {
	DatasetID string `json:"dataset_id"`
}

// Task is one authoring task. In DAG v1 every task is a dbt model.
type Task struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	DependsOn    []string `json:"depends_on"`
	Model        string   `json:"model"`
	Materialized string   `json:"materialized"`
	SQL          string   `json:"sql"`
}

// Output is one declared dataset output produced by a dbt model.
type Output struct {
	DatasetID string `json:"dataset_id"`
	Model     string `json:"model"`
}

// ValidationError is a stable, machine-readable DAG validation failure.
// Code is part of the API contract; Message is human-readable and may change.
type ValidationError struct {
	Code    string
	Message string
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Message
}

// Unwrap lets callers treat every validation failure as invalid argument while
// still extracting the stable Code with ValidationCode or errors.As.
func (e *ValidationError) Unwrap() error {
	return ErrInvalidArgument
}

// ValidationCode returns the stable DAG validation code carried by err, or ""
// when err is not a validation failure.
func ValidationCode(err error) string {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return validation.Code
	}
	return ""
}

func newValidationError(code, format string, args ...any) *ValidationError {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ParseDAG parses an authoring DAG into the v1 structure. Parsing is lenient by
// design: nil, empty and JSON null normalize to an empty v1 DAG, unknown fields
// are ignored, and a missing dag_version defaults to 1. Shape errors (not a
// JSON object) and a non-integer dag_version are rejected.
func ParseDAG(raw json.RawMessage) (DAG, error) {
	empty := DAG{DAGVersion: DAGVersionV1}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return empty, nil
	}
	if trimmed[0] != '{' {
		return DAG{}, fmt.Errorf("%w: dag must be a JSON object", ErrInvalidArgument)
	}
	if !json.Valid(trimmed) {
		return DAG{}, fmt.Errorf("%w: dag must be valid JSON", ErrInvalidArgument)
	}

	var envelope struct {
		DAGVersion json.RawMessage `json:"dag_version"`
		DatasetIDs []string        `json:"dataset_ids"`
		Inputs     []Input         `json:"inputs"`
		Tasks      []Task          `json:"tasks"`
		Outputs    []Output        `json:"outputs"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return DAG{}, fmt.Errorf("%w: dag must be a JSON object with supported fields", ErrInvalidArgument)
	}

	version := DAGVersionV1
	if rawVersion := bytes.TrimSpace(envelope.DAGVersion); len(rawVersion) > 0 &&
		!bytes.Equal(rawVersion, []byte("null")) {
		var parsed int
		if err := json.Unmarshal(rawVersion, &parsed); err != nil {
			return DAG{}, newValidationError(CodeInvalidDAGVersion, "dag_version must be an integer")
		}
		version = parsed
	}

	return DAG{
		DAGVersion: version,
		DatasetIDs: envelope.DatasetIDs,
		Inputs:     envelope.Inputs,
		Tasks:      envelope.Tasks,
		Outputs:    envelope.Outputs,
	}, nil
}

// ValidateDAGDefinition enforces the authoring DAG v1 contract used before a
// version is persisted or registered with the execution engine. Checks run in a
// fixed order so the first reported failure is deterministic. A DAG without
// tasks is valid: it renders as a single no-op flow task.
//
// The name ValidateDAG is taken by the lenient save-path object check in
// models.go; this strict, publish-path validator therefore carries a distinct
// name until the save-path check is folded into ParseDAG.
func ValidateDAGDefinition(dag DAG) error {
	if dag.DAGVersion != DAGVersionV1 {
		return newValidationError(
			CodeInvalidDAGVersion,
			"dag_version %d is not supported (want %d)", dag.DAGVersion, DAGVersionV1,
		)
	}

	// Task identity: non-empty and unique. DAG v1 has no dedicated
	// "empty task id" code, so both identity failures share duplicate_task_id.
	seen := make(map[string]struct{}, len(dag.Tasks))
	for index, task := range dag.Tasks {
		id := strings.TrimSpace(task.ID)
		if id == "" {
			return newValidationError(CodeDuplicateTaskID, "tasks[%d].id must not be empty", index)
		}
		if _, ok := seen[id]; ok {
			return newValidationError(CodeDuplicateTaskID, "duplicate task id %q", id)
		}
		seen[id] = struct{}{}
	}

	// Dependency references must resolve to a declared task id.
	for _, task := range dag.Tasks {
		for _, dependency := range task.DependsOn {
			reference := strings.TrimSpace(dependency)
			if reference == "" {
				return newValidationError(
					CodeMissingDependency,
					"task %q has an empty dependency reference", strings.TrimSpace(task.ID),
				)
			}
			if _, ok := seen[reference]; !ok {
				return newValidationError(
					CodeMissingDependency,
					"task %q depends on unknown task %q", strings.TrimSpace(task.ID), reference,
				)
			}
		}
	}

	// The dependency graph must be acyclic. Detection is deterministic: the
	// lexicographically smallest unresolved task id is reported.
	if blocked, cyclic := findDependencyCycle(dag.Tasks); cyclic {
		return newValidationError(CodeDependencyCycle, "dependency cycle involving task %q", blocked)
	}

	// Only dbt tasks are supported by DAG v1.
	for _, task := range dag.Tasks {
		if task.Type != TaskTypeDBT {
			return newValidationError(
				CodeUnknownTaskType,
				"task %q has unsupported type %q (want %q)",
				strings.TrimSpace(task.ID), task.Type, TaskTypeDBT,
			)
		}
	}

	// dbt tasks must declare the model they materialize and its SQL. The
	// reference is invalid, not merely incomplete, so it uses
	// invalid_sql_reference.
	for _, task := range dag.Tasks {
		if strings.TrimSpace(task.Model) == "" {
			return newValidationError(
				CodeInvalidSQLReference, "task %q: model is required for dbt tasks", strings.TrimSpace(task.ID),
			)
		}
		if strings.TrimSpace(task.SQL) == "" {
			return newValidationError(
				CodeInvalidSQLReference, "task %q: sql is required for dbt tasks", strings.TrimSpace(task.ID),
			)
		}
	}

	// Declared outputs must name a model declared by a dbt task and identify a
	// dataset. An empty dataset reference is a missing dependency reference;
	// a model that no task declares is an invalid model reference.
	declaredModels := make(map[string]struct{}, len(dag.Tasks))
	for _, task := range dag.Tasks {
		declaredModels[strings.TrimSpace(task.Model)] = struct{}{}
	}
	for index, output := range dag.Outputs {
		model := strings.TrimSpace(output.Model)
		if _, ok := declaredModels[model]; !ok {
			return newValidationError(
				CodeInvalidSQLReference,
				"outputs[%d].model %q does not match a declared dbt task model", index, output.Model,
			)
		}
		if strings.TrimSpace(output.DatasetID) == "" {
			return newValidationError(
				CodeMissingDependency, "outputs[%d].dataset_id must not be empty", index,
			)
		}
	}

	return nil
}

// findDependencyCycle runs a deterministic Kahn walk over the declared tasks
// (callers must have validated non-empty unique ids and existing references).
// When the walk cannot resolve every task, it returns the smallest unresolved
// id; the id is only used for a stable message.
func findDependencyCycle(tasks []Task) (string, bool) {
	indegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		indegree[strings.TrimSpace(task.ID)] = 0
	}
	for _, task := range tasks {
		id := strings.TrimSpace(task.ID)
		seen := make(map[string]struct{}, len(task.DependsOn))
		for _, dependency := range task.DependsOn {
			reference := strings.TrimSpace(dependency)
			if _, ok := seen[reference]; ok {
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

	resolved := 0
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		resolved++
		children := append([]string(nil), dependents[id]...)
		sort.Strings(children)
		for _, child := range children {
			indegree[child]--
			if indegree[child] == 0 {
				ready = insertSorted(ready, child)
			}
		}
	}
	if resolved == len(indegree) {
		return "", false
	}

	blocked := make([]string, 0, len(indegree)-resolved)
	for id, count := range indegree {
		if count > 0 {
			blocked = append(blocked, id)
		}
	}
	sort.Strings(blocked)
	return blocked[0], true
}

func insertSorted(sortedIDs []string, id string) []string {
	index := sort.SearchStrings(sortedIDs, id)
	sortedIDs = append(sortedIDs, "")
	copy(sortedIDs[index+1:], sortedIDs[index:])
	sortedIDs[index] = id
	return sortedIDs
}
