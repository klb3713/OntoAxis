package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

func runtimeManager(t *testing.T) (*service.Manager, *repo.MemoryStore, *kestra.MemoryAdapter, *time.Time) {
	t.Helper()
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	manager := service.New(store,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithReconcileThrottle(time.Millisecond),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	return manager, store, adapter, &now
}

func runtimePipeline(t *testing.T, manager *service.Manager, now time.Time) domain.Pipeline {
	t.Helper()
	dag, err := json.Marshal(map[string]any{
		"dag_version": 1,
		"tasks": []map[string]any{{
			"id": "model_orders", "type": "dbt", "model": "model_orders",
			"materialized": "table", "sql": "select 1 as id",
		}},
		"outputs": []map[string]any{{"dataset_id": "22222222-2222-4222-8222-222222222222", "model": "model_orders"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID: "11111111-1111-4111-8111-111111111111",
		Name:     "runtime_pipeline",
		Type:     domain.PipelineTypeBatch,
		DAG:      dag,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pipeline
}

func TestPublishRendersFilesAndFlow(t *testing.T) {
	manager, _, adapter, _ := runtimeManager(t)
	pipeline := runtimePipeline(t, manager, time.Now())
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID, PublishedBy: "tester",
	}); err != nil {
		t.Fatal(err)
	}
	flows := adapter.Flows()
	if len(flows) != 1 || len(flows[0].FlowYAML) == 0 {
		t.Fatalf("flows = %+v", flows)
	}
}

func TestTriggerRecordsExecutionAndReconcileSucceeds(t *testing.T) {
	manager, store, adapter, now := runtimeManager(t)
	pipeline := runtimePipeline(t, manager, *now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err != nil {
		t.Fatal(err)
	}
	run, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionID == "" || run.Status != domain.PipelineRunStatusRunning {
		t.Fatalf("run = %+v", run)
	}
	if !adapter.SetExecutionStatus(run.ExecutionID, "SUCCESS") {
		t.Fatal("execution not recorded")
	}
	*now = now.Add(time.Minute)
	got, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID, RunID: run.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.PipelineRunStatusSucceeded {
		t.Fatalf("status = %s want succeeded", got.Status)
	}
	outputs := store.RunOutputs(pipeline.TenantID, run.ID)
	if len(outputs) != 1 || outputs[0].DatasetID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("outputs = %+v", outputs)
	}
}

func TestPublishWithoutAdapterFailsClosed(t *testing.T) {
	store := repo.NewMemoryStore()
	manager := service.New(store)
	pipeline := runtimePipeline(t, manager, time.Now())
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err == nil || !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("publish without adapter error = %v, want ErrUnavailable", err)
	}
}

func TestLegacyRunWithoutExecutionFailsAfterTimeout(t *testing.T) {
	manager, store, _, now := runtimeManager(t)
	pipeline := runtimePipeline(t, manager, *now)
	legacy := domain.PipelineRun{
		ID:         "33333333-3333-4333-8333-333333333333",
		TenantID:   pipeline.TenantID,
		PipelineID: pipeline.ID,
		Version:    1,
		Status:     domain.PipelineRunStatusRunning,
		CreatedAt:  now.Add(-25 * time.Hour),
	}
	if _, err := store.CreateRun(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	got, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID, RunID: legacy.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.PipelineRunStatusFailed || got.FailureCode != "legacy_run_without_execution" {
		t.Fatalf("run = %+v", got)
	}
}

func TestPublishRejectsOutputDatasetWithoutResolver(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	manager := service.New(store, service.WithKestraAdapter(adapter), service.WithDBTCatalog("memory"), service.WithEnvironment("test")).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v want unavailable", err)
	}
}

func TestPublishRejectsNonexistentOutputDataset(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	manager := service.New(store, service.WithKestraAdapter(adapter), service.WithDBTCatalog("memory"), service.WithEnvironment("test"), service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{}})).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("err = %v want invalid argument", err)
	}
}

func TestReconcileMarksExecutionNotFoundAfterBoundedRetries(t *testing.T) {
	manager, _, adapter, now := runtimeManager(t)
	pipeline := runtimePipeline(t, manager, *now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID}); err != nil {
		t.Fatal(err)
	}
	run, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID})
	if err != nil {
		t.Fatal(err)
	}
	adapter.GetExecutionError = fmt.Errorf("%w: missing", kestra.ErrNotFound)
	for i := 0; i < 3; i++ {
		*now = now.Add(time.Second)
		got, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID, RunID: run.ID})
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && got.Status != domain.PipelineRunStatusRunning {
			t.Fatalf("attempt %d status = %s", i, got.Status)
		}
		if i == 2 && (got.Status != domain.PipelineRunStatusFailed || got.FailureCode != "execution_not_found") {
			t.Fatalf("final = %+v", got)
		}
	}
}

func TestRecoveryTimeoutMarksExecutionNotAccepted(t *testing.T) {
	manager, store, adapter, now := runtimeManager(t)
	pipeline := runtimePipeline(t, manager, *now)
	queued := domain.PipelineRun{
		ID:         "44444444-4444-4444-8444-444444444444",
		TenantID:   pipeline.TenantID,
		PipelineID: pipeline.ID,
		Version:    1,
		Status:     domain.PipelineRunStatusQueued,
		CreatedAt:  now.Add(-3 * time.Minute),
	}
	if _, err := store.CreateRun(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	adapter.FindExecutionError = fmt.Errorf("%w: missing", kestra.ErrNotFound)
	got, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID, RunID: queued.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.PipelineRunStatusFailed || got.FailureCode != "execution_not_accepted" {
		t.Fatalf("run = %+v", got)
	}
}

func TestReconcileRecordsJobsTotalMetric(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	metrics := observability.NewMetrics(nil)
	manager := service.New(store,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithReconcileThrottle(time.Millisecond),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
		service.WithMetrics(metrics),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID}); err != nil {
		t.Fatal(err)
	}
	run, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{TenantID: pipeline.TenantID, PipelineID: pipeline.ID, RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "ontoaxis_jobs_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, l := range metric.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["type"] == "pipeline_run_reconcile" && metric.GetCounter().GetValue() >= 1 {
				return
			}
		}
	}
	t.Fatal("expected pipeline_run_reconcile jobs_total metric")
}
