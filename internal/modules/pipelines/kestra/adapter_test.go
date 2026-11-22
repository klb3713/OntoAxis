package kestra_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
)

const tenantID = "11111111-1111-4111-8111-111111111111"

func TestMemoryAdapterRecordsRegisteredFlows(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	flow := kestra.Flow{
		TenantID:   tenantID,
		PipelineID: "22222222-2222-4222-8222-222222222222",
		Name:       "orders_daily",
		Version:    1,
		DAG:        json.RawMessage(`{"tasks":[]}`),
	}
	if err := adapter.RegisterFlow(context.Background(), flow); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	if err := adapter.RegisterFlow(context.Background(), flow); err != nil {
		t.Fatalf("RegisterFlow replace: %v", err)
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
	if err := adapter.RegisterFlow(context.Background(), kestra.Flow{}); err == nil {
		t.Fatal("expected error for empty flow")
	}
}

func TestMemoryAdapterTriggerRun(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	result, err := adapter.TriggerRun(context.Background(), kestra.RunRequest{
		TenantID:   tenantID,
		PipelineID: "22222222-2222-4222-8222-222222222222",
		Version:    1,
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
	if len(runs) != 1 || runs[0].Version != 1 {
		t.Fatalf("Runs = %+v", runs)
	}
}

func TestMemoryAdapterPropagatesConfiguredErrors(t *testing.T) {
	adapter := kestra.NewMemoryAdapter()
	registerErr := errors.New("register failed")
	adapter.RegisterError = registerErr
	if err := adapter.RegisterFlow(context.Background(), kestra.Flow{
		TenantID:   tenantID,
		PipelineID: "22222222-2222-4222-8222-222222222222",
		Name:       "flow",
	}); !errors.Is(err, registerErr) {
		t.Fatalf("RegisterFlow error = %v", err)
	}

	triggerErr := errors.New("trigger failed")
	adapter.TriggerError = triggerErr
	if _, err := adapter.TriggerRun(context.Background(), kestra.RunRequest{
		TenantID:   tenantID,
		PipelineID: "22222222-2222-4222-8222-222222222222",
	}); !errors.Is(err, triggerErr) {
		t.Fatalf("TriggerRun error = %v", err)
	}
}
