package domain_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

func dbtTask(id string, dependsOn ...string) domain.Task {
	return domain.Task{
		ID:        id,
		Type:      domain.TaskTypeDBT,
		DependsOn: dependsOn,
		Model:     "model_" + id,
		SQL:       "select 1 as id",
	}
}

func TestParseDAGLenientDefaults(t *testing.T) {
	for _, raw := range []json.RawMessage{
		nil,
		json.RawMessage(``),
		json.RawMessage(`   `),
		json.RawMessage(`null`),
		json.RawMessage(`{}`),
		json.RawMessage(`{"dag_version":null}`),
	} {
		dag, err := domain.ParseDAG(raw)
		if err != nil {
			t.Fatalf("ParseDAG(%q): %v", string(raw), err)
		}
		if dag.DAGVersion != domain.DAGVersionV1 {
			t.Fatalf("ParseDAG(%q).DAGVersion = %d, want %d", string(raw), dag.DAGVersion, domain.DAGVersionV1)
		}
		if len(dag.Tasks) != 0 || len(dag.Outputs) != 0 {
			t.Fatalf("ParseDAG(%q) = %+v, want empty DAG", string(raw), dag)
		}
		if err := domain.ValidateDAGDefinition(dag); err != nil {
			t.Fatalf("ValidateDAGDefinition(parsed %q): %v", string(raw), err)
		}
	}
}

func TestParseDAGFullDocument(t *testing.T) {
	raw := json.RawMessage(`{
		"dag_version": 1,
		"dataset_ids": ["11111111-1111-4111-8111-111111111111"],
		"inputs": [{"dataset_id": "22222222-2222-4222-8222-222222222222", "future": true}],
		"tasks": [
			{
				"id": "clean_orders",
				"type": "dbt",
				"depends_on": ["raw_orders"],
				"model": "clean_orders",
				"materialized": "table",
				"sql": "select * from {{ ref('raw_orders') }}",
				"unknown_task_field": {"ignored": true}
			},
			{
				"id": "raw_orders",
				"type": "dbt",
				"model": "raw_orders",
				"sql": "select 1"
			}
		],
		"outputs": [{"dataset_id": "33333333-3333-4333-8333-333333333333", "model": "clean_orders"}],
		"unknown_top_level": "ignored"
	}`)

	dag, err := domain.ParseDAG(raw)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}
	if dag.DAGVersion != 1 {
		t.Fatalf("DAGVersion = %d, want 1", dag.DAGVersion)
	}
	if len(dag.DatasetIDs) != 1 || dag.DatasetIDs[0] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("DatasetIDs = %v", dag.DatasetIDs)
	}
	if len(dag.Inputs) != 1 || dag.Inputs[0].DatasetID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("Inputs = %+v", dag.Inputs)
	}
	if len(dag.Tasks) != 2 {
		t.Fatalf("Tasks = %+v", dag.Tasks)
	}
	clean := dag.Tasks[0]
	if clean.ID != "clean_orders" || clean.Type != "dbt" || clean.Model != "clean_orders" ||
		clean.Materialized != "table" || clean.SQL == "" {
		t.Fatalf("task[0] = %+v", clean)
	}
	if len(clean.DependsOn) != 1 || clean.DependsOn[0] != "raw_orders" {
		t.Fatalf("task[0].DependsOn = %v", clean.DependsOn)
	}
	if len(dag.Outputs) != 1 || dag.Outputs[0].DatasetID != "33333333-3333-4333-8333-333333333333" ||
		dag.Outputs[0].Model != "clean_orders" {
		t.Fatalf("Outputs = %+v", dag.Outputs)
	}
	if err := domain.ValidateDAGDefinition(dag); err != nil {
		t.Fatalf("ValidateDAG: %v", err)
	}
}

func TestParseDAGRejectsMalformedJSON(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`[]`),
		json.RawMessage(`"text"`),
		json.RawMessage(`1`),
		json.RawMessage(`true`),
		json.RawMessage(`{"`),
		json.RawMessage(`{"tasks":"not-a-list"}`),
		json.RawMessage(`{"dag_version":1} trailing`),
	} {
		dag, err := domain.ParseDAG(raw)
		if err == nil {
			t.Fatalf("ParseDAG(%q) = %+v, want error", string(raw), dag)
		}
		if !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("ParseDAG(%q) = %v, want ErrInvalidArgument", string(raw), err)
		}
		if code := domain.ValidationCode(err); code != "" {
			t.Fatalf("ParseDAG(%q) code = %q, want empty", string(raw), code)
		}
	}
}

func TestParseDAGRejectsNonIntegerVersion(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"dag_version":"1"}`),
		json.RawMessage(`{"dag_version":1.5}`),
		json.RawMessage(`{"dag_version":true}`),
		json.RawMessage(`{"dag_version":{"value":1}}`),
	} {
		_, err := domain.ParseDAG(raw)
		if err == nil {
			t.Fatalf("ParseDAG(%q) expected error", string(raw))
		}
		if code := domain.ValidationCode(err); code != domain.CodeInvalidDAGVersion {
			t.Fatalf("ParseDAG(%q) code = %q, want %q", string(raw), code, domain.CodeInvalidDAGVersion)
		}
		if !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("ParseDAG(%q) = %v, want ErrInvalidArgument", string(raw), err)
		}
	}
}

func TestParseDAGUnsupportedVersionIsRejectedAtValidation(t *testing.T) {
	dag, err := domain.ParseDAG(json.RawMessage(`{"dag_version":2,"tasks":[]}`))
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}
	if dag.DAGVersion != 2 {
		t.Fatalf("DAGVersion = %d, want 2", dag.DAGVersion)
	}
	if code := domain.ValidationCode(domain.ValidateDAGDefinition(dag)); code != domain.CodeInvalidDAGVersion {
		t.Fatalf("ValidateDAG code = %q, want %q", code, domain.CodeInvalidDAGVersion)
	}
}

func TestValidateDAGDefinitionValid(t *testing.T) {
	valid := []struct {
		name string
		dag  domain.DAG
	}{
		{
			name: "no tasks stage 1 definition",
			dag:  domain.DAG{DAGVersion: 1, DatasetIDs: []string{"11111111-1111-4111-8111-111111111111"}},
		},
		{
			name: "single dbt task",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("clean_orders")}},
		},
		{
			name: "dependency chain declared out of order",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{
				dbtTask("clean_orders", "raw_orders"),
				dbtTask("raw_orders"),
				dbtTask("report", "clean_orders"),
			}},
		},
		{
			name: "duplicate dependency entries are harmless",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a"), dbtTask("b", "a", "a")}},
		},
		{
			name: "materialized is optional",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{
				{ID: "a", Type: domain.TaskTypeDBT, Model: "model_a", SQL: "select 1"},
			}},
		},
		{
			name: "declared output matches dbt task model",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("clean_orders")},
				Outputs: []domain.Output{{DatasetID: "33333333-3333-4333-8333-333333333333", Model: "model_clean_orders"}}},
		},
		{
			name: "input datasets are not resolved by DAG validation",
			dag: domain.DAG{DAGVersion: 1,
				Inputs: []domain.Input{{DatasetID: ""}},
				Tasks:  []domain.Task{dbtTask("a")}},
		},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := domain.ValidateDAGDefinition(test.dag); err != nil {
				t.Fatalf("ValidateDAG = %v, want nil", err)
			}
		})
	}
}

func TestValidateDAGDefinitionInvalid(t *testing.T) {
	twoTasks := func() []domain.Task {
		return []domain.Task{dbtTask("a"), dbtTask("b")}
	}
	invalid := []struct {
		name string
		dag  domain.DAG
		code string
	}{
		{
			name: "zero version",
			dag:  domain.DAG{},
			code: domain.CodeInvalidDAGVersion,
		},
		{
			name: "future version",
			dag:  domain.DAG{DAGVersion: 2},
			code: domain.CodeInvalidDAGVersion,
		},
		{
			name: "empty task id",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("")}},
			code: domain.CodeDuplicateTaskID,
		},
		{
			name: "whitespace task id",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("   ")}},
			code: domain.CodeDuplicateTaskID,
		},
		{
			name: "duplicate task id",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a"), dbtTask("a")}},
			code: domain.CodeDuplicateTaskID,
		},
		{
			name: "duplicate task id ignoring surrounding whitespace",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a"), dbtTask(" a ")}},
			code: domain.CodeDuplicateTaskID,
		},
		{
			name: "missing dependency",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a", "ghost")}},
			code: domain.CodeMissingDependency,
		},
		{
			name: "empty dependency reference",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a"), dbtTask("b", "  ")}},
			code: domain.CodeMissingDependency,
		},
		{
			name: "two node cycle",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a", "b"), dbtTask("b", "a")}},
			code: domain.CodeDependencyCycle,
		},
		{
			name: "self cycle",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a", "a")}},
			code: domain.CodeDependencyCycle,
		},
		{
			name: "disjoint cycles",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{
				dbtTask("d", "c"), dbtTask("c", "d"),
				dbtTask("b", "a"), dbtTask("a", "b"),
			}},
			code: domain.CodeDependencyCycle,
		},
		{
			name: "unknown task type",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{
				{ID: "a", Type: "python", Model: "m", SQL: "print(1)"},
			}},
			code: domain.CodeUnknownTaskType,
		},
		{
			name: "empty task type",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a"), {ID: "b", Model: "m", SQL: "select 1"}}},
			code: domain.CodeUnknownTaskType,
		},
		{
			name: "case sensitive task type",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{{ID: "a", Type: "DBT", Model: "m", SQL: "select 1"}}},
			code: domain.CodeUnknownTaskType,
		},
		{
			name: "dbt task without model",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{{ID: "a", Type: domain.TaskTypeDBT, SQL: "select 1"}}},
			code: domain.CodeInvalidSQLReference,
		},
		{
			name: "dbt task without sql",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{{ID: "a", Type: domain.TaskTypeDBT, Model: "m"}}},
			code: domain.CodeInvalidSQLReference,
		},
		{
			name: "dbt task with whitespace sql",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{{ID: "a", Type: domain.TaskTypeDBT, Model: "m", SQL: "   "}}},
			code: domain.CodeInvalidSQLReference,
		},
		{
			name: "output model not declared by any task",
			dag: domain.DAG{DAGVersion: 1, Tasks: twoTasks(),
				Outputs: []domain.Output{{DatasetID: "33333333-3333-4333-8333-333333333333", Model: "unknown_model"}}},
			code: domain.CodeInvalidSQLReference,
		},
		{
			name: "output dataset id empty",
			dag: domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a")},
				Outputs: []domain.Output{{DatasetID: "  ", Model: "model_a"}}},
			code: domain.CodeMissingDependency,
		},
		{
			name: "version failure takes precedence over shape errors",
			dag:  domain.DAG{DAGVersion: 9, Tasks: []domain.Task{dbtTask("a"), dbtTask("a")}},
			code: domain.CodeInvalidDAGVersion,
		},
		{
			name: "identity failure takes precedence over cycle",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{dbtTask("a", "a"), dbtTask("a")}},
			code: domain.CodeDuplicateTaskID,
		},
		{
			name: "type failure takes precedence over missing model",
			dag:  domain.DAG{DAGVersion: 1, Tasks: []domain.Task{{ID: "a", Type: "python"}}},
			code: domain.CodeUnknownTaskType,
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			err := domain.ValidateDAGDefinition(test.dag)
			if err == nil {
				t.Fatalf("ValidateDAG = nil, want %q", test.code)
			}
			if code := domain.ValidationCode(err); code != test.code {
				t.Fatalf("ValidationCode = %q, want %q (err: %v)", code, test.code, err)
			}
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("ValidateDAG error %v is not ErrInvalidArgument", err)
			}
		})
	}
}

func TestValidateDAGDefinitionCycleDetectionIsDeterministic(t *testing.T) {
	dag := domain.DAG{DAGVersion: 1, Tasks: []domain.Task{
		dbtTask("delta", "charlie"),
		dbtTask("bravo", "alpha"),
		dbtTask("charlie", "delta"),
		dbtTask("alpha", "bravo"),
		dbtTask("echo"),
	}}
	var first string
	for i := 0; i < 25; i++ {
		err := domain.ValidateDAGDefinition(dag)
		if err == nil {
			t.Fatal("ValidateDAG = nil, want cycle error")
		}
		if code := domain.ValidationCode(err); code != domain.CodeDependencyCycle {
			t.Fatalf("ValidationCode = %q, want %q", code, domain.CodeDependencyCycle)
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("cycle error changed: %q != %q", err.Error(), first)
		}
	}
	if !strings.Contains(first, "alpha") {
		t.Fatalf("cycle error = %q, want deterministic smallest blocked task alpha", first)
	}
}

func TestValidationCode(t *testing.T) {
	if code := domain.ValidationCode(nil); code != "" {
		t.Fatalf("ValidationCode(nil) = %q", code)
	}
	if code := domain.ValidationCode(errors.New("plain")); code != "" {
		t.Fatalf("ValidationCode(plain) = %q", code)
	}

	validation := &domain.ValidationError{Code: domain.CodeDependencyCycle, Message: "cycle"}
	if code := domain.ValidationCode(validation); code != domain.CodeDependencyCycle {
		t.Fatalf("ValidationCode = %q", code)
	}
	if !errors.Is(validation, domain.ErrInvalidArgument) {
		t.Fatal("ValidationError must unwrap to ErrInvalidArgument")
	}
	wrapped := fmt.Errorf("validate dag: %w", validation)
	if code := domain.ValidationCode(wrapped); code != domain.CodeDependencyCycle {
		t.Fatalf("ValidationCode(wrapped) = %q", code)
	}
	if validation.Error() != "cycle" {
		t.Fatalf("Error() = %q", validation.Error())
	}
	emptyMessage := &domain.ValidationError{Code: domain.CodeInvalidDAGVersion}
	if emptyMessage.Error() != domain.CodeInvalidDAGVersion {
		t.Fatalf("Error() = %q, want code fallback", emptyMessage.Error())
	}
}

func TestParseDAGNoTaskCompatibility(t *testing.T) {
	definitions := []json.RawMessage{
		json.RawMessage(`{"dataset_ids":[]}`),
		json.RawMessage(`{"dataset_ids":["11111111-1111-4111-8111-111111111111"],"inputs":[{"dataset_id":"22222222-2222-4222-8222-222222222222"}]}`),
	}
	for _, raw := range definitions {
		dag, err := domain.ParseDAG(raw)
		if err != nil {
			t.Fatalf("ParseDAG(%s): %v", raw, err)
		}
		if len(dag.Tasks) != 0 {
			t.Fatalf("ParseDAG(%s).Tasks = %+v, want none", raw, dag.Tasks)
		}
		if err := domain.ValidateDAGDefinition(dag); err != nil {
			t.Fatalf("ValidateDAGDefinition(%s): %v", raw, err)
		}
	}
}
