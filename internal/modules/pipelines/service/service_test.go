package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

func fixedNow() time.Time {
	return time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
}

func requireErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func createPipeline(t *testing.T, manager *service.Manager, tenantID, name string) domain.Pipeline {
	t.Helper()
	pipeline, err := manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID:    tenantID,
		Name:        name,
		DisplayName: "Display " + name,
		Type:        domain.PipelineTypeBatch,
		DAG:         json.RawMessage(`{"dataset_ids":[]}`),
	})
	if err != nil {
		t.Fatalf("CreatePipeline(%q): %v", name, err)
	}
	return pipeline
}

func TestCreatePipelineDefaultsAndValidation(t *testing.T) {
	store := repo.NewMemoryStore()
	manager := service.New(store).WithNow(fixedNow)

	pipeline := createPipeline(t, manager, tenantA, "daily_orders")
	if pipeline.Version != 0 {
		t.Fatalf("version = %d, want 0", pipeline.Version)
	}
	if pipeline.Status != domain.PipelineStatusDraft {
		t.Fatalf("status = %q, want draft", pipeline.Status)
	}
	if pipeline.Type != domain.PipelineTypeBatch {
		t.Fatalf("type = %q, want batch", pipeline.Type)
	}
	if string(pipeline.DAG) != `{"dataset_ids":[]}` {
		t.Fatalf("dag = %s", pipeline.DAG)
	}
	parsed, err := uuid.Parse(pipeline.ID)
	if err != nil {
		t.Fatalf("id is not a UUID: %v", err)
	}
	if parsed.Version() != 7 {
		t.Fatalf("id version = %d, want 7", parsed.Version())
	}
	if !pipeline.CreatedAt.Equal(fixedNow()) || !pipeline.UpdatedAt.Equal(fixedNow()) {
		t.Fatalf("timestamps = %s / %s", pipeline.CreatedAt, pipeline.UpdatedAt)
	}

	_, err = manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID: tenantA, Name: "daily_orders", Type: domain.PipelineTypeBatch,
	})
	requireErrorIs(t, err, domain.ErrAlreadyExists)

	if _, err := manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID: tenantB, Name: "daily_orders", Type: domain.PipelineTypeBatch,
	}); err != nil {
		t.Fatalf("same name in another tenant should be allowed: %v", err)
	}

	invalid := []service.CreatePipelineRequest{
		{TenantID: tenantA, Name: "bad type", Type: domain.PipelineType("streaming")},
		{TenantID: tenantA, Name: "array_dag", Type: domain.PipelineTypeBatch, DAG: json.RawMessage(`[]`)},
		{TenantID: tenantA, Name: "not json", Type: domain.PipelineTypeBatch, DAG: json.RawMessage(`{`)},
		{TenantID: tenantA, Name: "", Type: domain.PipelineTypeBatch},
		{TenantID: "", Name: "no_tenant", Type: domain.PipelineTypeBatch},
		{TenantID: "not-a-uuid", Name: "bad_tenant", Type: domain.PipelineTypeBatch},
	}
	for _, request := range invalid {
		if _, err := manager.CreatePipeline(context.Background(), request); err == nil {
			t.Fatalf("CreatePipeline(%+v) unexpectedly succeeded", request)
		}
	}
}

func TestGetAndUpdateAreTenantScoped(t *testing.T) {
	manager := service.New(repo.NewMemoryStore()).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "tenant_scoped")

	got, err := manager.GetPipeline(context.Background(), tenantA, pipeline.ID)
	if err != nil || got.ID != pipeline.ID {
		t.Fatalf("GetPipeline = %+v, %v", got, err)
	}
	_, err = manager.GetPipeline(context.Background(), tenantB, pipeline.ID)
	requireErrorIs(t, err, domain.ErrNotFound)

	displayName := "Updated"
	dag := json.RawMessage(`{"dataset_ids":[]}`)
	updated, err := manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, DisplayName: &displayName, DAG: dag,
	})
	if err != nil {
		t.Fatalf("UpdatePipeline: %v", err)
	}
	if updated.DisplayName != "Updated" {
		t.Fatalf("display name = %q", updated.DisplayName)
	}
	_, err = manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantB, PipelineID: pipeline.ID, DisplayName: &displayName,
	})
	requireErrorIs(t, err, domain.ErrNotFound)

	_, err = manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, DAG: json.RawMessage(`[]`),
	})
	requireErrorIs(t, err, domain.ErrInvalidArgument)

	if err := manager.DeletePipeline(context.Background(), tenantA, pipeline.ID); err != nil {
		t.Fatalf("DeletePipeline: %v", err)
	}
	_, err = manager.GetPipeline(context.Background(), tenantA, pipeline.ID)
	requireErrorIs(t, err, domain.ErrNotFound)
	page, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{TenantID: tenantA})
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if page.Page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("soft-deleted pipeline is visible: %+v", page)
	}
}

func TestArchivedPipelineRejectsUpdateAndPublish(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	manager := service.New(store, service.WithKestraAdapter(adapter)).WithNow(fixedNow)
	now := fixedNow()
	archived := domain.Pipeline{
		ID: uuid.NewString(), TenantID: tenantA, Name: "archived", Type: domain.PipelineTypeBatch,
		DAG: json.RawMessage(`{}`), Status: domain.PipelineStatusArchived, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreatePipeline(context.Background(), archived); err != nil {
		t.Fatalf("seed archived pipeline: %v", err)
	}
	_, err := manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantA, PipelineID: archived.ID,
	})
	requireErrorIs(t, err, domain.ErrInvalidState)
	_, _, err = manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: archived.ID,
	})
	requireErrorIs(t, err, domain.ErrInvalidState)
	if len(adapter.Flows()) != 0 {
		t.Fatalf("archived pipeline registered a flow")
	}
}

func TestPublishCreatesImmutableVersions(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	manager := service.New(store, service.WithKestraAdapter(adapter)).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "publishable")

	published, version, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, PublishedBy: "tester",
	})
	if err != nil {
		t.Fatalf("PublishPipeline: %v", err)
	}
	if published.Version != 1 || published.Status != domain.PipelineStatusPublished {
		t.Fatalf("published = version %d status %q", published.Version, published.Status)
	}
	if version.Version != 1 || version.PublishedBy != "tester" {
		t.Fatalf("version = %+v", version)
	}
	if len(adapter.Flows()) != 1 {
		t.Fatalf("adapter flows = %d, want 1", len(adapter.Flows()))
	}

	newDAG := json.RawMessage(`{"dataset_ids":[],"stages":[{"id":"transform"}]}`)
	updated, err := manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, DAG: newDAG,
	})
	if err != nil {
		t.Fatalf("UpdatePipeline: %v", err)
	}
	if updated.Status != domain.PipelineStatusDraft {
		t.Fatalf("status after DAG edit = %q, want draft", updated.Status)
	}
	if _, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("trigger after DAG edit = %v, want invalid state", err)
	}
	republished, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	if err != nil {
		t.Fatalf("second PublishPipeline: %v", err)
	}
	if republished.Version != 2 {
		t.Fatalf("republished version = %d, want 2", republished.Version)
	}
	versions := store.Versions(pipeline.ID)
	if len(versions) != 2 {
		t.Fatalf("stored versions = %d, want 2", len(versions))
	}
	if string(versions[0].DAG) != `{"dataset_ids":[]}` {
		t.Fatalf("version 1 DAG changed: %s", versions[0].DAG)
	}
	if string(versions[1].DAG) != string(newDAG) {
		t.Fatalf("version 2 DAG = %s", versions[1].DAG)
	}
	if len(adapter.Flows()) != 2 {
		t.Fatalf("adapter flows = %d, want 2", len(adapter.Flows()))
	}

	displayName := "Renamed"
	renamed, err := manager.UpdatePipeline(context.Background(), service.UpdatePipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, DisplayName: &displayName,
	})
	if err != nil {
		t.Fatalf("display-only UpdatePipeline: %v", err)
	}
	if renamed.Status != domain.PipelineStatusPublished || renamed.Version != 2 {
		t.Fatalf("display-only edit changed state: %+v", renamed)
	}
}

func TestPublishFailsClosedWithoutAdapter(t *testing.T) {
	manager := service.New(repo.NewMemoryStore()).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "no_adapter")
	_, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrUnavailable)
}

func TestTriggerPipelineRunLifecycle(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	manager := service.New(store, service.WithKestraAdapter(adapter)).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "runnable")

	_, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrInvalidState)
	if adapter.TriggeredCount != 0 {
		t.Fatalf("draft trigger called adapter")
	}

	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	}); err != nil {
		t.Fatalf("PublishPipeline: %v", err)
	}
	run, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	if err != nil {
		t.Fatalf("TriggerPipelineRun: %v", err)
	}
	if run.Status != domain.PipelineRunStatusRunning || run.TriggerType != domain.TriggerTypeManual {
		t.Fatalf("run = %+v", run)
	}
	if run.Version != 1 || run.StartedAt == nil {
		t.Fatalf("run version/started_at = %d / %v", run.Version, run.StartedAt)
	}
	if adapter.TriggeredCount != 1 {
		t.Fatalf("adapter trigger count = %d, want 1", adapter.TriggeredCount)
	}

	got, err := manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, RunID: run.ID,
	})
	if err != nil || got.ID != run.ID {
		t.Fatalf("GetPipelineRun = %+v, %v", got, err)
	}
	_, err = manager.GetPipelineRun(context.Background(), service.GetPipelineRunRequest{
		TenantID: tenantB, PipelineID: pipeline.ID, RunID: run.ID,
	})
	requireErrorIs(t, err, domain.ErrNotFound)

	runs, err := manager.ListPipelineRuns(context.Background(), service.ListPipelineRunsRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, Page: 1, PerPage: 20,
	})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if runs.Page.Total != 1 || len(runs.Items) != 1 {
		t.Fatalf("runs page = %+v", runs)
	}
}

func TestTriggerAdapterFailurePersistsFailedRun(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	manager := service.New(store, service.WithKestraAdapter(adapter)).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "adapter_failure")
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	}); err != nil {
		t.Fatalf("PublishPipeline: %v", err)
	}
	adapter.TriggerError = errors.New("provider exploded with secret token")
	_, err := manager.TriggerPipelineRun(context.Background(), service.TriggerPipelineRunRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrUnavailable)

	page, err := manager.ListPipelineRuns(context.Background(), service.ListPipelineRunsRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, PerPage: 10,
	})
	if err != nil {
		t.Fatalf("ListPipelineRuns: %v", err)
	}
	if page.Page.Total != 1 || page.Items[0].Status != domain.PipelineRunStatusFailed {
		t.Fatalf("failed run = %+v", page.Items)
	}
	if page.Items[0].Error == "" || page.Items[0].Error == "provider exploded with secret token" {
		t.Fatalf("run error leaks adapter details: %q", page.Items[0].Error)
	}
}

func TestDatasetReferencesAreValidated(t *testing.T) {
	datasetID := uuid.NewString()
	dag := json.RawMessage(`{"dataset_ids":["` + datasetID + `"]}`)
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()

	manager := service.New(store, service.WithKestraAdapter(adapter)).WithNow(fixedNow)
	pipeline, err := manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID: tenantA, Name: "with_dataset", Type: domain.PipelineTypeBatch, DAG: dag,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	_, _, err = manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrUnavailable)

	missing := service.New(store, service.WithKestraAdapter(adapter),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{}})).WithNow(fixedNow)
	_, _, err = missing.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrInvalidArgument)

	failing := service.New(store, service.WithKestraAdapter(adapter),
		service.WithDatasetResolver(fakeDatasetResolver{err: errors.New("lookup failed")})).WithNow(fixedNow)
	_, _, err = failing.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	requireErrorIs(t, err, domain.ErrUnavailable)

	ok := service.New(store, service.WithKestraAdapter(adapter),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{datasetID: true}})).WithNow(fixedNow)
	published, _, err := ok.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	if err != nil {
		t.Fatalf("publish with existing dataset: %v", err)
	}
	if published.Version != 1 {
		t.Fatalf("version = %d", published.Version)
	}
}

func TestScheduleLifecycle(t *testing.T) {
	store := repo.NewMemoryStore()
	manager := service.New(store, service.WithKestraAdapter(kestra.NewMemoryAdapter())).WithNow(fixedNow)
	pipeline := createPipeline(t, manager, tenantA, "scheduled")

	schedule, err := manager.CreateSchedule(context.Background(), service.CreateScheduleRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, Cron: "0 * * * *", Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if !schedule.Enabled || schedule.NextRunAt == nil {
		t.Fatalf("schedule = %+v", schedule)
	}
	if schedule.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q", schedule.Timezone)
	}

	_, err = manager.CreateSchedule(context.Background(), service.CreateScheduleRequest{
		TenantID: tenantA, PipelineID: pipeline.ID, Cron: "0 * * * *", Timezone: "UTC",
	})
	requireErrorIs(t, err, domain.ErrAlreadyExists)

	invalid := []service.CreateScheduleRequest{
		{TenantID: tenantA, PipelineID: pipeline.ID, Cron: "bad"},
		{TenantID: tenantA, PipelineID: pipeline.ID, Cron: "* * * * *", Timezone: "Mars/Olympus"},
		{TenantID: tenantA, PipelineID: uuid.NewString(), Cron: "* * * * *"},
		{TenantID: "", PipelineID: pipeline.ID, Cron: "* * * * *"},
	}
	for _, request := range invalid {
		if _, err := manager.CreateSchedule(context.Background(), request); err == nil {
			t.Fatalf("CreateSchedule(%+v) unexpectedly succeeded", request)
		}
	}

	paused, err := manager.PauseSchedule(context.Background(), tenantA, schedule.ID)
	if err != nil {
		t.Fatalf("PauseSchedule: %v", err)
	}
	if paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("paused schedule = %+v", paused)
	}
	resumed, err := manager.ResumeSchedule(context.Background(), tenantA, schedule.ID)
	if err != nil {
		t.Fatalf("ResumeSchedule: %v", err)
	}
	if !resumed.Enabled || resumed.NextRunAt == nil {
		t.Fatalf("resumed schedule = %+v", resumed)
	}

	cron := "30 9 * * MON-FRI"
	updated, err := manager.UpdateSchedule(context.Background(), service.UpdateScheduleRequest{
		TenantID: tenantA, ScheduleID: schedule.ID, Cron: &cron,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.Cron != cron || updated.NextRunAt == nil {
		t.Fatalf("updated schedule = %+v", updated)
	}

	list, err := manager.ListSchedules(context.Background(), service.ListSchedulesRequest{
		TenantID: tenantA, PipelineID: pipeline.ID,
	})
	if err != nil || list.Page.Total != 1 {
		t.Fatalf("ListSchedules = %+v, %v", list, err)
	}
	if _, err := manager.GetSchedule(context.Background(), tenantB, schedule.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant GetSchedule error = %v", err)
	}
	if err := manager.DeleteSchedule(context.Background(), tenantA, schedule.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	_, err = manager.GetSchedule(context.Background(), tenantA, schedule.ID)
	requireErrorIs(t, err, domain.ErrNotFound)
}

func TestListPaginationAndFilters(t *testing.T) {
	manager := service.New(repo.NewMemoryStore()).WithNow(fixedNow)
	createPipeline(t, manager, tenantA, "first")
	createPipeline(t, manager, tenantA, "second")
	third, err := manager.CreatePipeline(context.Background(), service.CreatePipelineRequest{
		TenantID: tenantA, Name: "third", Type: domain.PipelineTypeIncremental,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	_ = third

	page, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{
		TenantID: tenantA, Page: 2, PerPage: 1,
	})
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(page.Items) != 1 || page.Page.Total != 3 || page.Page.TotalPages != 3 || page.Page.Page != 2 {
		t.Fatalf("page = %+v", page)
	}

	filtered, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{
		TenantID: tenantA, Type: domain.PipelineTypeIncremental,
	})
	if err != nil || filtered.Page.Total != 1 {
		t.Fatalf("type filter = %+v, %v", filtered, err)
	}
	if filtered.Page.PerPage != 50 {
		t.Fatalf("default per_page = %d, want 50", filtered.Page.PerPage)
	}

	if _, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{
		TenantID: tenantA, PerPage: 201,
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("per_page=201 error = %v", err)
	}
	if _, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{
		TenantID: tenantA, Page: -1,
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("page=-1 error = %v", err)
	}
	if _, err := manager.ListPipelines(context.Background(), service.ListPipelinesRequest{
		TenantID: tenantA, Status: domain.PipelineStatus("unknown"),
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("unknown status error = %v", err)
	}
}

type fakeDatasetResolver struct {
	exists map[string]bool
	err    error
}

func (f fakeDatasetResolver) DatasetExists(_ context.Context, _, datasetID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.exists[datasetID], nil
}
