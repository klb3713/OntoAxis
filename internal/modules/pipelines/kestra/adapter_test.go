package kestra_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
)

const (
	tenantID   = "11111111-1111-4111-8111-111111111111"
	pipelineID = "22222222-2222-4222-8222-222222222222"
)

// Compile-time interface conformance for both adapters.
var (
	_ kestra.Adapter = (*kestra.MemoryAdapter)(nil)
	_ kestra.Adapter = (*kestra.HTTPAdapter)(nil)
)

func TestMemoryAdapterRecordsRegisteredFlows(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	flow := kestra.Flow{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Name:       "orders_daily",
		Version:    1,
		DAG:        json.RawMessage(`{"tasks":[]}`),
	}
	ref, err := adapter.RegisterFlow(context.Background(), flow)
	if err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	if ref.Namespace != "oa_11111111111141118111111111111111.22222222222242228222222222222222" {
		t.Fatalf("Namespace = %q", ref.Namespace)
	}
	if ref.FlowID != "22222222222242228222222222222222" || ref.Revision != 1 {
		t.Fatalf("FlowRef = %+v", ref)
	}
	secondRef, err := adapter.RegisterFlow(context.Background(), flow)
	if err != nil {
		t.Fatalf("RegisterFlow replace: %v", err)
	}
	if secondRef != ref {
		t.Fatalf("repeated RegisterFlow = %+v, want %+v", secondRef, ref)
	}
	flows := adapter.Flows()
	if len(flows) != 1 {
		t.Fatalf("Flows = %d, want 1", len(flows))
	}
	if flows[0].Name != "orders_daily" || flows[0].Version != 1 {
		t.Fatalf("recorded flow = %+v", flows[0])
	}
}

func TestMemoryAdapterRejectsInvalidFlow(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	if _, err := adapter.RegisterFlow(context.Background(), kestra.Flow{}); err == nil {
		t.Fatal("expected error for empty flow")
	}
}

func TestMemoryAdapterPublishesAndDeletesFiles(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	files := []kestra.File{
		{Path: "pipeline_abc/v1/dbt_project.yml", Content: []byte("name: orders")},
		{Path: "pipeline_abc/v1/models/orders.sql", Content: []byte("select 1")},
	}
	if err := adapter.PublishFiles(context.Background(), "oa_tenant.pipeline", files); err != nil {
		t.Fatalf("PublishFiles: %v", err)
	}
	// Re-publishing a path replaces content instead of duplicating it.
	if err := adapter.PublishFiles(context.Background(), "oa_tenant.pipeline", []kestra.File{
		{Path: "pipeline_abc/v1/dbt_project.yml", Content: []byte("name: orders_v2")},
	}); err != nil {
		t.Fatalf("PublishFiles replace: %v", err)
	}
	stored := adapter.Files("oa_tenant.pipeline")
	if len(stored) != 2 {
		t.Fatalf("Files = %+v", stored)
	}
	if string(stored[0].Content) != "name: orders_v2" {
		t.Fatalf("dbt_project.yml content = %q", stored[0].Content)
	}
	if err := adapter.DeleteFiles(context.Background(), "oa_tenant.pipeline", []string{"pipeline_abc/v1/models/missing.sql"}); err != nil {
		t.Fatalf("DeleteFiles missing path: %v", err)
	}
	if err := adapter.DeleteFiles(context.Background(), "oa_tenant.pipeline", []string{"pipeline_abc/v1/models/orders.sql"}); err != nil {
		t.Fatalf("DeleteFiles: %v", err)
	}
	if stored := adapter.Files("oa_tenant.pipeline"); len(stored) != 1 {
		t.Fatalf("Files after delete = %+v", stored)
	}
}

func TestMemoryAdapterTriggerRun(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	result, err := adapter.TriggerRun(context.Background(), kestra.RunRequest{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Version:    1,
		RunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	if result.ExecutionID == "" {
		t.Fatal("expected execution id")
	}
	if result.Status != "queued" {
		t.Fatalf("Status = %q, want queued", result.Status)
	}
	if adapter.TriggeredCount != 1 {
		t.Fatalf("TriggeredCount = %d", adapter.TriggeredCount)
	}
	runs := adapter.Runs()
	if len(runs) != 1 || runs[0].Version != 1 || runs[0].RunID != "run-1" {
		t.Fatalf("Runs = %+v", runs)
	}

	execution, err := adapter.FindExecutionByRun(context.Background(), tenantID, "run-1")
	if err != nil {
		t.Fatalf("FindExecutionByRun: %v", err)
	}
	if execution.ID != result.ExecutionID {
		t.Fatalf("found execution = %+v, want id %s", execution, result.ExecutionID)
	}
	if execution.Labels["ontoaxis.tenant_id"] != tenantID {
		t.Fatalf("labels = %+v", execution.Labels)
	}
	if _, err := adapter.FindExecutionByRun(context.Background(), tenantID, "missing-run"); !errors.Is(err, kestra.ErrNotFound) {
		t.Fatalf("FindExecutionByRun missing = %v, want ErrNotFound", err)
	}
}

func TestMemoryAdapterFetchesAndCancelsExecution(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	result, err := adapter.TriggerRun(context.Background(), kestra.RunRequest{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Version:    1,
		RunID:      "run-2",
	})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	ref := kestra.ExecutionRef{ID: result.ExecutionID}
	execution, err := adapter.GetExecution(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if execution.Status != "queued" || execution.Labels["ontoaxis.run_id"] != "run-2" {
		t.Fatalf("execution = %+v", execution)
	}
	if err := adapter.CancelExecution(context.Background(), ref); err != nil {
		t.Fatalf("CancelExecution: %v", err)
	}
	execution, err = adapter.GetExecution(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetExecution after cancel: %v", err)
	}
	if execution.Status != "KILLED" {
		t.Fatalf("Status after cancel = %q, want KILLED", execution.Status)
	}
	if _, err := adapter.GetExecution(context.Background(), kestra.ExecutionRef{ID: "missing"}); !errors.Is(err, kestra.ErrNotFound) {
		t.Fatalf("GetExecution missing = %v, want ErrNotFound", err)
	}
}

func TestMemoryAdapterPropagatesConfiguredErrors(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	registerErr := errors.New("register failed")
	adapter.RegisterError = registerErr
	if _, err := adapter.RegisterFlow(context.Background(), kestra.Flow{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Name:       "flow",
	}); !errors.Is(err, registerErr) {
		t.Fatalf("RegisterFlow error = %v", err)
	}

	triggerErr := errors.New("trigger failed")
	adapter.TriggerError = triggerErr
	if _, err := adapter.TriggerRun(context.Background(), kestra.RunRequest{
		TenantID:   tenantID,
		PipelineID: pipelineID,
	}); !errors.Is(err, triggerErr) {
		t.Fatalf("TriggerRun error = %v", err)
	}

	publishErr := errors.New("publish failed")
	adapter.PublishFilesError = publishErr
	if err := adapter.PublishFiles(context.Background(), "oa_tenant.pipeline", []kestra.File{{Path: "a.txt"}}); !errors.Is(err, publishErr) {
		t.Fatalf("PublishFiles error = %v", err)
	}

	deleteErr := errors.New("delete failed")
	adapter.DeleteFilesError = deleteErr
	if err := adapter.DeleteFiles(context.Background(), "oa_tenant.pipeline", []string{"a.txt"}); !errors.Is(err, deleteErr) {
		t.Fatalf("DeleteFiles error = %v", err)
	}

	getErr := errors.New("get failed")
	adapter.GetExecutionError = getErr
	if _, err := adapter.GetExecution(context.Background(), kestra.ExecutionRef{ID: "x"}); !errors.Is(err, getErr) {
		t.Fatalf("GetExecution error = %v", err)
	}

	findErr := errors.New("find failed")
	adapter.FindExecutionError = findErr
	if _, err := adapter.FindExecutionByRun(context.Background(), tenantID, "run"); !errors.Is(err, findErr) {
		t.Fatalf("FindExecutionByRun error = %v", err)
	}

	cancelErr := errors.New("cancel failed")
	adapter.CancelExecutionError = cancelErr
	if err := adapter.CancelExecution(context.Background(), kestra.ExecutionRef{ID: "x"}); !errors.Is(err, cancelErr) {
		t.Fatalf("CancelExecution error = %v", err)
	}
}
