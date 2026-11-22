package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

func pipeline(id, tenant, name string, createdAt time.Time) domain.Pipeline {
	return domain.Pipeline{
		ID:        id,
		TenantID:  tenant,
		Name:      name,
		Type:      domain.PipelineTypeBatch,
		DAG:       json.RawMessage(`{"tasks":[]}`),
		Status:    domain.PipelineStatusDraft,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func TestMemoryStoreTenantUniquenessAndSoftDelete(t *testing.T) {
	store := repo.NewMemoryStore()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	first := pipeline(uuid.NewString(), tenantA, "shared", now)
	if _, err := store.CreatePipeline(context.Background(), first); err != nil {
		t.Fatalf("create: %v", err)
	}
	duplicate := pipeline(uuid.NewString(), tenantA, "shared", now.Add(time.Second))
	if _, err := store.CreatePipeline(context.Background(), duplicate); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate create = %v", err)
	}
	otherTenant := pipeline(uuid.NewString(), tenantB, "shared", now.Add(time.Second))
	if _, err := store.CreatePipeline(context.Background(), otherTenant); err != nil {
		t.Fatalf("other tenant create: %v", err)
	}

	// Returned values must not alias the stored DAG.
	first.DAG[2] = 'X'
	stored, err := store.GetPipeline(context.Background(), tenantA, first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(stored.DAG) != `{"tasks":[]}` {
		t.Fatalf("stored DAG was mutated: %s", stored.DAG)
	}

	if err := store.DeletePipeline(context.Background(), tenantA, first.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetPipeline(context.Background(), tenantA, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted = %v", err)
	}
	_, total, err := store.ListPipelines(context.Background(), service.PipelineFilter{
		TenantID: tenantA, Page: 1, PerPage: 50,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 {
		t.Fatalf("deleted pipeline still listed: %d", total)
	}
}

func TestMemoryStoreListOrderAndPagination(t *testing.T) {
	store := repo.NewMemoryStore()
	base := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		item := pipeline(uuid.NewString(), tenantA, name, base.Add(time.Duration(i)*time.Minute))
		if _, err := store.CreatePipeline(context.Background(), item); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	items, total, err := store.ListPipelines(context.Background(), service.PipelineFilter{
		TenantID: tenantA, Page: 2, PerPage: 1,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 1 || items[0].Name != "second" {
		t.Fatalf("page 2 = %+v total %d", items, total)
	}
}

func TestMemoryStorePublishIsVersioned(t *testing.T) {
	store := repo.NewMemoryStore()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	item := pipeline(uuid.NewString(), tenantA, "versioned", now)
	if _, err := store.CreatePipeline(context.Background(), item); err != nil {
		t.Fatalf("create: %v", err)
	}
	item.Version = 1
	item.Status = domain.PipelineStatusPublished
	version := domain.PipelineVersion{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: item.ID, Version: 1,
		DAG: json.RawMessage(`{"v":1}`), PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := store.PublishPipeline(context.Background(), item, version); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, _, err := store.PublishPipeline(context.Background(), item, version); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate publish = %v", err)
	}
	stale := item
	stale.Version = 3
	if _, _, err := store.PublishPipeline(context.Background(), stale, domain.PipelineVersion{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: item.ID, Version: 3,
	}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("stale publish = %v", err)
	}
	versions := store.Versions(item.ID)
	if len(versions) != 1 || string(versions[0].DAG) != `{"v":1}` {
		t.Fatalf("versions = %+v", versions)
	}
}

func TestMemoryStoreRunAndScheduleScoping(t *testing.T) {
	store := repo.NewMemoryStore()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	pipelineID := uuid.NewString()
	started := now.Add(time.Minute)
	run := domain.PipelineRun{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Version: 1, Status: domain.PipelineRunStatusSucceeded, TriggerType: domain.TriggerTypeManual,
		StartedAt: &started, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.GetRun(context.Background(), tenantB, pipelineID, run.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant run = %v", err)
	}
	runs, total, err := store.ListRuns(context.Background(), service.RunFilter{
		TenantID: tenantA, PipelineID: pipelineID, Page: 1, PerPage: 10, Status: domain.PipelineRunStatusSucceeded,
	})
	if err != nil || total != 1 || len(runs) != 1 {
		t.Fatalf("list runs = %+v total %d err %v", runs, total, err)
	}

	schedule := domain.Schedule{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Cron: "0 * * * *", Timezone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateSchedule(context.Background(), schedule); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	duplicate := schedule
	duplicate.ID = uuid.NewString()
	if _, err := store.CreateSchedule(context.Background(), duplicate); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate schedule = %v", err)
	}
	second := schedule
	second.ID = uuid.NewString()
	second.Cron = "30 * * * *"
	if _, err := store.CreateSchedule(context.Background(), second); err != nil {
		t.Fatalf("second schedule: %v", err)
	}
	second.Cron = schedule.Cron
	if _, err := store.UpdateSchedule(context.Background(), second); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("conflicting schedule update = %v", err)
	}
	if _, err := store.GetSchedule(context.Background(), tenantB, schedule.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant schedule = %v", err)
	}
}

func TestMemoryStoreRunExecutionFields(t *testing.T) {
	store := repo.NewMemoryStore()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	pipelineID := uuid.NewString()
	started := now.Add(time.Minute)
	reconciled := now.Add(2 * time.Minute)
	run := domain.PipelineRun{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Version: 1, Status: domain.PipelineRunStatusRunning, TriggerType: domain.TriggerTypeManual,
		ExecutionID: "exec-42", FailureCode: "invalid_sql_reference", AttemptCount: 2,
		LastReconciledAt: &reconciled, StartedAt: &started, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	stored, err := store.GetRun(context.Background(), tenantA, pipelineID, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.ExecutionID != "exec-42" || stored.FailureCode != "invalid_sql_reference" || stored.AttemptCount != 2 {
		t.Fatalf("execution fields not persisted: %+v", stored)
	}
	if stored.LastReconciledAt == nil || !stored.LastReconciledAt.Equal(reconciled) {
		t.Fatalf("last_reconciled_at not persisted: %+v", stored.LastReconciledAt)
	}
	// Returned pointers must not alias stored state.
	*stored.LastReconciledAt = reconciled.Add(time.Hour)
	reloaded, err := store.GetRun(context.Background(), tenantA, pipelineID, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if !reloaded.LastReconciledAt.Equal(reconciled) {
		t.Fatalf("stored last_reconciled_at was mutated through the returned copy: %v", reloaded.LastReconciledAt)
	}
	runs, total, err := store.ListRuns(context.Background(), service.RunFilter{
		TenantID: tenantA, PipelineID: pipelineID, Page: 1, PerPage: 10,
	})
	if err != nil || total != 1 {
		t.Fatalf("list runs = %+v total %d err %v", runs, total, err)
	}
	if runs[0].ExecutionID != "exec-42" || runs[0].AttemptCount != 2 {
		t.Fatalf("list runs lost execution fields: %+v", runs[0])
	}
}

func TestMemoryStoreUpdateRunConditional(t *testing.T) {
	store := repo.NewMemoryStore()
	ctx := context.Background()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	pipelineID := uuid.NewString()
	run := domain.PipelineRun{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Version: 1, Status: domain.PipelineRunStatusQueued, TriggerType: domain.TriggerTypeManual,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	started := now.Add(time.Minute)
	firstReconcile := now.Add(2 * time.Minute)
	update := run
	update.Status = domain.PipelineRunStatusRunning
	update.ExecutionID = "exec-1"
	update.AttemptCount = 1
	update.StartedAt = &started
	update.LastReconciledAt = &firstReconcile
	update.UpdatedAt = now.Add(3 * time.Minute)
	got, err := store.UpdateRunConditional(ctx, update)
	if err != nil {
		t.Fatalf("queued->running: %v", err)
	}
	if got.Status != domain.PipelineRunStatusRunning || got.ExecutionID != "exec-1" || got.AttemptCount != 1 {
		t.Fatalf("unexpected transition result: %+v", got)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(started) {
		t.Fatalf("started_at not set: %v", got.StartedAt)
	}

	// A later reconcile may raise the attempt counter and advance
	// last_reconciled_at, but must not rewrite started_at or clear references.
	laterReconcile := now.Add(5 * time.Minute)
	laterStarted := now.Add(4 * time.Minute)
	update = got
	update.AttemptCount = 2
	update.LastReconciledAt = &laterReconcile
	update.StartedAt = &laterStarted
	update.ExecutionID = ""
	update.UpdatedAt = now.Add(6 * time.Minute)
	got, err = store.UpdateRunConditional(ctx, update)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got.AttemptCount != 2 || !got.LastReconciledAt.Equal(laterReconcile) {
		t.Fatalf("reconcile merge = %+v", got)
	}
	if !got.StartedAt.Equal(started) {
		t.Fatalf("started_at must be set once, got %v want %v", got.StartedAt, started)
	}
	if got.ExecutionID != "exec-1" {
		t.Fatalf("empty execution id must keep the stored value, got %q", got.ExecutionID)
	}

	finished := now.Add(7 * time.Minute)
	terminal := got
	terminal.Status = domain.PipelineRunStatusSucceeded
	terminal.FinishedAt = &finished
	terminal.FailureCode = ""
	terminal.ExecutionID = ""
	terminal.UpdatedAt = now.Add(8 * time.Minute)
	got, err = store.UpdateRunConditional(ctx, terminal)
	if err != nil {
		t.Fatalf("running->succeeded: %v", err)
	}
	if got.Status != domain.PipelineRunStatusSucceeded || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Fatalf("terminal transition = %+v", got)
	}
	if got.ExecutionID != "exec-1" || got.AttemptCount != 2 {
		t.Fatalf("terminal transition lost execution fields: %+v", got)
	}

	// Terminal runs never transition again, whatever the payload says.
	regression := got
	regression.Status = domain.PipelineRunStatusRunning
	regression.UpdatedAt = now.Add(9 * time.Minute)
	if _, err := store.UpdateRunConditional(ctx, regression); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("terminal regression = %v", err)
	}

	// Cross-tenant and unknown runs are not visible to the CAS.
	crossTenant := update
	crossTenant.TenantID = tenantB
	if _, err := store.UpdateRunConditional(ctx, crossTenant); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant update = %v", err)
	}
	missing := update
	missing.ID = uuid.NewString()
	if _, err := store.UpdateRunConditional(ctx, missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing run update = %v", err)
	}

	// A queued run may transition straight to a terminal state (adapter
	// callback failing before a start event).
	direct := domain.PipelineRun{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Version: 1, Status: domain.PipelineRunStatusQueued, TriggerType: domain.TriggerTypeManual,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateRun(ctx, direct); err != nil {
		t.Fatalf("create direct run: %v", err)
	}
	direct.Status = domain.PipelineRunStatusFailed
	direct.FailureCode = "execution_failed"
	direct.UpdatedAt = now.Add(time.Minute)
	if _, err := store.UpdateRunConditional(ctx, direct); err != nil {
		t.Fatalf("queued->failed: %v", err)
	}
}

func TestMemoryStoreRunInputsAndOutputs(t *testing.T) {
	store := repo.NewMemoryStore()
	ctx := context.Background()
	now := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	pipelineID := uuid.NewString()
	run := domain.PipelineRun{
		ID: uuid.NewString(), TenantID: tenantA, PipelineID: pipelineID,
		Version: 1, Status: domain.PipelineRunStatusRunning, TriggerType: domain.TriggerTypeManual,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	inputs := []domain.PipelineRunInput{
		{PipelineRunID: run.ID, TenantID: tenantA, DatasetID: uuid.NewString(), CreatedAt: now},
		{PipelineRunID: run.ID, TenantID: tenantA, DatasetID: uuid.NewString(), CreatedAt: now},
	}
	// Duplicate rows inside the same batch must not duplicate lineage.
	inputs = append(inputs, inputs[0])
	if err := store.CreateRunInputs(ctx, inputs); err != nil {
		t.Fatalf("create inputs: %v", err)
	}
	if got := store.RunInputs(tenantA, run.ID); len(got) != 2 {
		t.Fatalf("inputs = %d, want 2: %+v", len(got), got)
	}
	// A terminal callback replay writes the same inputs again; this is a no-op.
	if err := store.CreateRunInputs(ctx, inputs); err != nil {
		t.Fatalf("replay inputs: %v", err)
	}
	if got := store.RunInputs(tenantA, run.ID); len(got) != 2 {
		t.Fatalf("replayed inputs duplicated lineage: %+v", got)
	}

	outputs := []domain.PipelineRunOutput{
		{PipelineRunID: run.ID, TenantID: tenantA, DatasetID: uuid.NewString(), Model: "model_a",
			Catalog: "memory", Schema: "oa_t11111111_paaaaaaaa", Table: "model_a", CreatedAt: now},
		{PipelineRunID: run.ID, TenantID: tenantA, DatasetID: uuid.NewString(), Model: "model_b",
			Catalog: "memory", Schema: "oa_t11111111_paaaaaaaa", Table: "model_b", CreatedAt: now},
	}
	if err := store.CreateRunOutputs(ctx, outputs); err != nil {
		t.Fatalf("create outputs: %v", err)
	}
	stored := store.RunOutputs(tenantA, run.ID)
	if len(stored) != 2 {
		t.Fatalf("outputs = %d, want 2: %+v", len(stored), stored)
	}
	if stored[0].Model != "model_a" || stored[0].Catalog != "memory" ||
		stored[0].Schema != "oa_t11111111_paaaaaaaa" || stored[0].Table != "model_a" {
		t.Fatalf("output relation not persisted: %+v", stored[0])
	}
	// Replaying the same (run, dataset) pairs must not duplicate outputs and
	// must keep the first recorded relation (ON CONFLICT DO NOTHING semantics).
	replay := append([]domain.PipelineRunOutput(nil), outputs...)
	replay[0].Model = "model_a_renamed"
	replay[0].Table = "model_a_renamed"
	if err := store.CreateRunOutputs(ctx, replay); err != nil {
		t.Fatalf("replay outputs: %v", err)
	}
	stored = store.RunOutputs(tenantA, run.ID)
	if len(stored) != 2 {
		t.Fatalf("replayed outputs duplicated lineage: %+v", stored)
	}
	if stored[0].Model != "model_a" {
		t.Fatalf("replay rewrote the output relation: %+v", stored[0])
	}

	// The write APIs reject runs that are unknown or owned by another tenant.
	unknown := []domain.PipelineRunInput{{PipelineRunID: uuid.NewString(), TenantID: tenantA, DatasetID: uuid.NewString()}}
	if err := store.CreateRunInputs(ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run inputs = %v", err)
	}
	crossTenant := []domain.PipelineRunOutput{{PipelineRunID: run.ID, TenantID: tenantB, DatasetID: uuid.NewString()}}
	if err := store.CreateRunOutputs(ctx, crossTenant); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant outputs = %v", err)
	}
	if len(store.RunInputs(tenantB, run.ID)) != 0 || len(store.RunOutputs(tenantB, run.ID)) != 0 {
		t.Fatal("cross-tenant inspection must return nothing")
	}
	if err := store.CreateRunInputs(ctx, nil); err != nil {
		t.Fatalf("empty inputs: %v", err)
	}
	if err := store.CreateRunOutputs(ctx, nil); err != nil {
		t.Fatalf("empty outputs: %v", err)
	}
}
