package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

type failingPublishRepo struct {
	*repo.MemoryStore
	fail bool
}

func (r *failingPublishRepo) PublishPipeline(ctx context.Context, pipeline domain.Pipeline, version domain.PipelineVersion) (domain.Pipeline, domain.PipelineVersion, error) {
	if r.fail {
		return domain.Pipeline{}, domain.PipelineVersion{}, domain.ErrUnavailable
	}
	return r.MemoryStore.PublishPipeline(ctx, pipeline, version)
}

type recordingAdapter struct {
	*kestra.MemoryAdapter
	registered        []kestra.Flow
	deleteFlowCalls   []kestra.FlowRef
	deletedFileCalls  int
	registerCalls     int
	publishFilesCalls int
}

func (a *recordingAdapter) RegisterFlow(ctx context.Context, flow kestra.Flow) (kestra.FlowRef, error) {
	a.registerCalls++
	a.registered = append(a.registered, flow)
	return a.MemoryAdapter.RegisterFlow(ctx, flow)
}

func (a *recordingAdapter) DeleteFlow(ctx context.Context, ref kestra.FlowRef) error {
	a.deleteFlowCalls = append(a.deleteFlowCalls, ref)
	return a.MemoryAdapter.DeleteFlow(ctx, ref)
}

func (a *recordingAdapter) DeleteFiles(ctx context.Context, namespace string, paths []string) error {
	a.deletedFileCalls++
	return a.MemoryAdapter.DeleteFiles(ctx, namespace, paths)
}

func (a *recordingAdapter) PublishFiles(ctx context.Context, namespace string, files []kestra.File) error {
	a.publishFilesCalls++
	return a.MemoryAdapter.PublishFiles(ctx, namespace, files)
}

type partialFilesAdapter struct {
	*recordingAdapter
	failPublishFiles bool
}

func (a *partialFilesAdapter) PublishFiles(ctx context.Context, namespace string, files []kestra.File) error {
	a.publishFilesCalls++
	if a.failPublishFiles && len(files) > 1 {
		if err := a.MemoryAdapter.PublishFiles(ctx, namespace, files[:1]); err != nil {
			return err
		}
		return errors.New("injected partial file upload failure")
	}
	return a.MemoryAdapter.PublishFiles(ctx, namespace, files)
}

type flakyRegisterAdapter struct {
	*recordingAdapter
	failures int
}

func (a *flakyRegisterAdapter) RegisterFlow(ctx context.Context, flow kestra.Flow) (kestra.FlowRef, error) {
	a.registerCalls++
	if a.failures > 0 {
		a.failures--
		return kestra.FlowRef{}, errors.New("injected register failure")
	}
	a.registered = append(a.registered, flow)
	return a.MemoryAdapter.RegisterFlow(ctx, flow)
}

func TestPublishFailureRestoresPreviousFlowAndFiles(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := &recordingAdapter{MemoryAdapter: kestra.NewMemoryAdapter()}
	failing := &failingPublishRepo{MemoryStore: store}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	manager := service.New(failing,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	first, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID, PublishedBy: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 {
		t.Fatalf("first version = %d", first.Version)
	}
	failing.fail = true
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID, PublishedBy: "tester",
	}); err == nil || !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("publish failure = %v, want ErrUnavailable", err)
	}
	if len(adapter.registered) < 2 {
		t.Fatalf("register calls = %d", len(adapter.registered))
	}
	last := adapter.registered[len(adapter.registered)-1]
	if last.Version != 1 {
		t.Fatalf("compensated flow version = %d, want 1", last.Version)
	}
	current, err := store.GetPipeline(context.Background(), pipeline.TenantID, pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 1 {
		t.Fatalf("stored version = %d, want 1", current.Version)
	}
}

func TestFirstPublishFailureDeletesFlowAndFiles(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := &recordingAdapter{MemoryAdapter: kestra.NewMemoryAdapter()}
	failing := &failingPublishRepo{MemoryStore: store, fail: true}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	manager := service.New(failing,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err == nil {
		t.Fatal("expected publish failure")
	}
	if len(adapter.deleteFlowCalls) != 1 {
		t.Fatalf("delete flow calls = %d, want 1", len(adapter.deleteFlowCalls))
	}
	if adapter.deleteFlowCalls[0].Revision != 1 {
		t.Fatalf("deleted revision = %d, want 1", adapter.deleteFlowCalls[0].Revision)
	}
	if adapter.deletedFileCalls == 0 {
		t.Fatal("expected namespace file cleanup")
	}
}

func TestPublishFilesPartialFailureRestoresPreviousProject(t *testing.T) {
	store := repo.NewMemoryStore()
	base := &recordingAdapter{MemoryAdapter: kestra.NewMemoryAdapter()}
	adapter := &partialFilesAdapter{recordingAdapter: base}
	failing := &failingPublishRepo{MemoryStore: store}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	manager := service.New(failing,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err != nil {
		t.Fatal(err)
	}
	adapter.failPublishFiles = true
	_, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	})
	if err == nil {
		t.Fatal("expected publish failure")
	}
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	// The previous project must be republished and the last Flow registration
	// must remain the committed version.
	if base.publishFilesCalls < 2 {
		t.Fatalf("publish files calls = %d", base.publishFilesCalls)
	}
	if got := base.registered[len(base.registered)-1].Version; got != 1 {
		t.Fatalf("compensated flow version = %d, want 1", got)
	}
}

func TestPublishRecoversAfterEngineFailureOnlyOnce(t *testing.T) {
	store := repo.NewMemoryStore()
	base := &recordingAdapter{MemoryAdapter: kestra.NewMemoryAdapter()}
	adapter := &flakyRegisterAdapter{recordingAdapter: base, failures: 1}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	manager := service.New(store,
		service.WithKestraAdapter(adapter),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err == nil {
		t.Fatal("expected first publish to fail")
	}
	published, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Version != 1 {
		t.Fatalf("recovered version = %d, want 1", published.Version)
	}
	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err != nil {
		t.Fatal(err)
	}
	current, _ := store.GetPipeline(context.Background(), pipeline.TenantID, pipeline.ID)
	if current.Version != 2 {
		t.Fatalf("version after second success = %d, want 2", current.Version)
	}
}

func TestPublishCompensationFailureIsWarnedAndStillFails(t *testing.T) {
	store := repo.NewMemoryStore()
	base := &recordingAdapter{MemoryAdapter: kestra.NewMemoryAdapter()}
	base.MemoryAdapter.DeleteFlowError = errors.New("injected delete flow failure")
	failing := &failingPublishRepo{MemoryStore: store, fail: true}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	manager := service.New(failing,
		service.WithKestraAdapter(base),
		service.WithDBTCatalog("memory"),
		service.WithEnvironment("test"),
		service.WithDatasetResolver(fakeDatasetResolver{exists: map[string]bool{"22222222-2222-4222-8222-222222222222": true}}),
	).WithNow(func() time.Time { return now })
	pipeline := runtimePipeline(t, manager, now)

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, _, err := manager.PublishPipeline(context.Background(), service.PublishPipelineRequest{
		TenantID: pipeline.TenantID, PipelineID: pipeline.ID,
	}); err == nil || !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if !strings.Contains(logs.String(), "pipeline publish compensation failed") {
		t.Fatalf("missing compensation warning: %s", logs.String())
	}
}
