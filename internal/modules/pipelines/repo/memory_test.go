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
