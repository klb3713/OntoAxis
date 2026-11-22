package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

func TestParsePipelineType(t *testing.T) {
	for _, raw := range []string{"batch", "faster", "incremental", "BATCH"} {
		parsed, err := domain.ParsePipelineType(raw)
		if err != nil {
			t.Fatalf("ParsePipelineType(%q): %v", raw, err)
		}
		if !parsed.Valid() {
			t.Fatalf("ParsePipelineType(%q) returned invalid type %q", raw, parsed)
		}
	}
	for _, raw := range []string{"", "streaming", "batch!"} {
		if _, err := domain.ParsePipelineType(raw); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("ParsePipelineType(%q) = %v, want invalid argument", raw, err)
		}
	}
}

func TestStatusValidity(t *testing.T) {
	for _, status := range []domain.PipelineStatus{
		domain.PipelineStatusDraft, domain.PipelineStatusPublished, domain.PipelineStatusArchived,
	} {
		if !status.Valid() {
			t.Fatalf("pipeline status %q should be valid", status)
		}
	}
	if domain.PipelineStatus("deleted").Valid() {
		t.Fatal("unknown pipeline status should be invalid")
	}
	for _, status := range []domain.PipelineRunStatus{
		domain.PipelineRunStatusQueued, domain.PipelineRunStatusRunning, domain.PipelineRunStatusSucceeded,
		domain.PipelineRunStatusFailed, domain.PipelineRunStatusCancelled,
	} {
		if !status.Valid() {
			t.Fatalf("run status %q should be valid", status)
		}
	}
	if domain.PipelineRunStatus("done").Valid() {
		t.Fatal("unknown run status should be invalid")
	}
}

func TestValidateDAG(t *testing.T) {
	valid := []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"dataset_ids":["a"],"inputs":[{"dataset_id":"b"}]}`),
	}
	for _, dag := range valid {
		if err := domain.ValidateDAG(dag); err != nil {
			t.Fatalf("ValidateDAG(%s): %v", dag, err)
		}
	}
	invalid := []json.RawMessage{
		nil,
		json.RawMessage(``),
		json.RawMessage(`[]`),
		json.RawMessage(`null`),
		json.RawMessage(`{"`),
		json.RawMessage(`"text"`),
	}
	for _, dag := range invalid {
		if err := domain.ValidateDAG(dag); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("ValidateDAG(%s) = %v, want invalid argument", dag, err)
		}
	}
}

func TestDatasetIDsFromDAG(t *testing.T) {
	dag := json.RawMessage(`{
		"dataset_ids": ["11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"],
		"inputs": [
			{"dataset_id": "22222222-2222-4222-8222-222222222222"},
			{"dataset_id": "33333333-3333-4333-8333-333333333333"},
			{"other": "ignored"}
		]
	}`)
	got := domain.DatasetIDsFromDAG(dag)
	want := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	}
	if len(got) != len(want) {
		t.Fatalf("DatasetIDsFromDAG = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DatasetIDsFromDAG[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if ids := domain.DatasetIDsFromDAG(json.RawMessage(`{"dataset_ids":[]}`)); len(ids) != 0 {
		t.Fatalf("empty dataset ids = %v", ids)
	}
	if ids := domain.DatasetIDsFromDAG(json.RawMessage(`not-json`)); len(ids) != 0 {
		t.Fatalf("malformed dag ids = %v", ids)
	}
}
