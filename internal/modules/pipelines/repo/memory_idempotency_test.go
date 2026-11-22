package repo

import (
	"context"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

func TestMemoryIdempotencyExpiryAndFirstWin(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	expired := service.IdempotencyRecord{TenantID: "t1", Operation: "pipeline.publish", KeyHash: "k1", RequestHash: "h1", ResponseStatus: 200, ResponseBody: []byte(`{}`), ExpiresAt: time.Now().Add(-time.Minute)}
	if _, found, err := store.LookupIdempotency(ctx, "t1", "pipeline.publish", "k1"); err != nil || found {
		t.Fatalf("expired lookup found=%v err=%v", found, err)
	}
	if ok, err := store.SaveIdempotency(ctx, expired); err != nil || !ok {
		t.Fatalf("save expired=%v err=%v", ok, err)
	}
	if _, found, err := store.LookupIdempotency(ctx, "t1", "pipeline.publish", "k1"); err != nil || found {
		t.Fatalf("expired record must not replay: found=%v err=%v", found, err)
	}
	live := expired
	live.ExpiresAt = time.Now().Add(time.Hour)
	if ok, err := store.SaveIdempotency(ctx, live); err != nil || !ok {
		t.Fatalf("save live=%v err=%v", ok, err)
	}
	if ok, err := store.SaveIdempotency(ctx, live); err != nil || ok {
		t.Fatalf("second save must be first-wins: ok=%v err=%v", ok, err)
	}
	if rec, found, err := store.LookupIdempotency(ctx, "t1", "pipeline.publish", "k1"); err != nil || !found || rec.RequestHash != "h1" {
		t.Fatalf("live lookup rec=%+v found=%v err=%v", rec, found, err)
	}
}

func TestMemoryMarkSucceededWithOutputsAtomic(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	pipeline := domain.Pipeline{ID: "p1", TenantID: "11111111-1111-4111-8111-111111111111", Name: "p", Status: domain.PipelineStatusPublished, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if _, err := store.CreatePipeline(ctx, pipeline); err != nil {
		t.Fatal(err)
	}
	run := domain.PipelineRun{ID: "r1", TenantID: pipeline.TenantID, PipelineID: "p1", Version: 1, Status: domain.PipelineRunStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	update := run
	update.Status = domain.PipelineRunStatusSucceeded
	bad := domain.PipelineRunOutput{PipelineRunID: "r1", TenantID: "22222222-2222-4222-8222-222222222222", DatasetID: "d1", Model: "m"}
	if _, err := store.MarkSucceededWithOutputs(ctx, update, []domain.PipelineRunOutput{bad}); err == nil {
		t.Fatal("expected tenant mismatch error")
	}
	got, err := store.GetRun(ctx, pipeline.TenantID, "p1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.PipelineRunStatusRunning {
		t.Fatalf("run status = %s, want running", got.Status)
	}
	store.mu.RLock()
	n := len(store.runOutputs["r1"])
	store.mu.RUnlock()
	if n != 0 {
		t.Fatalf("outputs = %d", n)
	}
}
