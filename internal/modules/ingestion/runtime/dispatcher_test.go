package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

func newTestDispatcher(t *testing.T, store *fakeStore, reader ConnectorReader, jobRuntime JobRuntime, mutate func(*DispatcherOptions)) *Dispatcher {
	t.Helper()
	opts := DispatcherOptions{
		Interval:      time.Hour,
		MaxBatch:      10,
		LeaseDuration: 30 * time.Second,
		MaxAttempts:   5,
		BackoffBase:   time.Second,
		LeaseOwner:    "test-owner",
		Clock:         store.clock.Now,
		Sleep:         func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		Rand:          func() float64 { return 0.5 },
		Classifier:    DefaultClassify,
		Logger:        discardLogger(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	dispatcher, err := NewDispatcher(store, reader, jobRuntime, opts)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	return dispatcher
}

func requireJobStatus(t *testing.T, store *fakeStore, jobID string, want domain.JobStatus) {
	t.Helper()
	job := store.getJob(jobID)
	if job == nil {
		t.Fatalf("job %s not found in fake store", jobID)
	}
	if job.Status != want {
		t.Fatalf("job %s status = %q, want %q", jobID, job.Status, want)
	}
}

func TestDispatchOnceSubmitsQueuedJob(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)
	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}

	job := store.getJob("job-1")
	if job.Status != domain.StatusRunning {
		t.Fatalf("job status = %q, want running", job.Status)
	}
	if job.RuntimeRef != "runtime-connector-1" {
		t.Fatalf("runtime ref = %q, want runtime-connector-1", job.RuntimeRef)
	}
	if job.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", job.Attempt)
	}
	if job.LeaseOwner != "" || job.LeaseUntil != nil {
		t.Fatalf("lease not cleared after submit: owner=%q until=%v", job.LeaseOwner, job.LeaseUntil)
	}
	if job.StartedAt == nil {
		t.Fatal("started_at not recorded")
	}

	submits := jobRuntime.submitCalls()
	if len(submits) != 1 {
		t.Fatalf("submit calls = %d, want 1", len(submits))
	}
	if submits[0].TenantID != "tenant-a" || submits[0].ConnectorID != "connector-1" {
		t.Fatalf("submit call = %+v, want tenant-a/connector-1", submits[0])
	}

	run := store.getRun("job-1")
	if run.Status != domain.StatusRunning {
		t.Fatalf("run status = %q, want running", run.Status)
	}
	if run.Stats[domain.StatsPhase] != domain.PhaseSnapshotting {
		t.Fatalf("run phase = %v, want %q", run.Stats[domain.StatsPhase], domain.PhaseSnapshotting)
	}
	if run.Stats[domain.StatsRuntimeRef] != "runtime-connector-1" {
		t.Fatalf("run runtime_ref = %v", run.Stats[domain.StatsRuntimeRef])
	}
	if run.Stats[domain.StatsAttempt] != 1 {
		t.Fatalf("run attempt = %v, want 1", run.Stats[domain.StatsAttempt])
	}
}

func TestDispatchOnceWithoutQueuedJobs(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, newFakeConnectorReader(), jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if jobRuntime.submitCount() != 0 {
		t.Fatalf("submit calls = %d, want 0", jobRuntime.submitCount())
	}
}

func TestDispatchRetriesRetryableFailureThenSucceeds(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	attempts := 0
	jobRuntime := newFakeJobRuntime()
	jobRuntime.submitFunc = func(_ context.Context, _ string, _ *connectordomain.Connector) (string, error) {
		attempts++
		if attempts <= 2 {
			return "", Retryable(errors.New("upstream timeout"))
		}
		return "runtime-connector-1", nil
	}
	var sleeps []time.Duration
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, func(opts *DispatcherOptions) {
		opts.Sleep = func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}
	})

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
	job := store.getJob("job-1")
	if job.Attempt != 3 {
		t.Fatalf("attempt = %d, want 3", job.Attempt)
	}
	if jobRuntime.submitCount() != 3 {
		t.Fatalf("submit calls = %d, want 3", jobRuntime.submitCount())
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep calls = %v, want 2", sleeps)
	}
	if sleeps[0] != time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("backoff sleeps = %v, want [1s 2s]", sleeps)
	}
	run := store.getRun("job-1")
	if run.Stats[domain.StatsAttempts] != 2 {
		t.Fatalf("recorded attempts = %v, want 2", run.Stats[domain.StatsAttempts])
	}
	if recorded, ok := run.Stats[domain.StatsLastError].(string); !ok || !strings.Contains(recorded, "upstream timeout") {
		t.Fatalf("recorded last_error = %v, want it to contain upstream timeout", run.Stats[domain.StatsLastError])
	}
}

func TestDispatchPermanentFailureMarksJobFailed(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	jobRuntime := newFakeJobRuntime()
	jobRuntime.submitFunc = func(_ context.Context, _ string, _ *connectordomain.Connector) (string, error) {
		return "", Permanent(errors.New("HTTP 401 unauthorized"))
	}
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusFailed)
	run := store.getRun("job-1")
	if !strings.Contains(run.Error, "submit_permanent_failure:") {
		t.Fatalf("run error = %q, want submit_permanent_failure prefix", run.Error)
	}
	if jobRuntime.submitCount() != 1 {
		t.Fatalf("submit calls = %d, want 1", jobRuntime.submitCount())
	}
}

func TestDispatchRetryExhaustionMarksJobFailed(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	jobRuntime := newFakeJobRuntime()
	jobRuntime.submitFunc = func(_ context.Context, _ string, _ *connectordomain.Connector) (string, error) {
		return "", Retryable(errors.New("connection reset"))
	}
	var sleeps []time.Duration
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, func(opts *DispatcherOptions) {
		opts.MaxAttempts = 3
		opts.Sleep = func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}
	})

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusFailed)
	run := store.getRun("job-1")
	if !strings.Contains(run.Error, "submit_retries_exhausted:") {
		t.Fatalf("run error = %q, want submit_retries_exhausted prefix", run.Error)
	}
	if jobRuntime.submitCount() != 3 {
		t.Fatalf("submit calls = %d, want 3", jobRuntime.submitCount())
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep calls = %v, want 2", sleeps)
	}
}

func TestDispatchConcurrentCyclesSubmitOnlyOnce(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- dispatcher.DispatchOnce(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent DispatchOnce() error = %v", err)
		}
	}
	if jobRuntime.submitCount() != 1 {
		t.Fatalf("submit calls = %d, want exactly 1", jobRuntime.submitCount())
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
}

func TestDispatchInactiveConnectorFails(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	connector := newActiveConnector("tenant-a", "connector-1")
	connector.Status = connectordomain.StatusPaused
	reader.add(connector)
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusFailed)
	run := store.getRun("job-1")
	if !strings.Contains(run.Error, "connector_not_active") {
		t.Fatalf("run error = %q, want connector_not_active", run.Error)
	}
	if jobRuntime.submitCount() != 0 {
		t.Fatalf("submit calls = %d, want 0", jobRuntime.submitCount())
	}
}

func TestDispatchMissingConnectorFails(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, newFakeConnectorReader(), jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusFailed)
	run := store.getRun("job-1")
	if !strings.Contains(run.Error, "connector_not_found") {
		t.Fatalf("run error = %q, want connector_not_found", run.Error)
	}
}

func TestDispatchConnectorReadErrorLeavesJobQueued(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.setError(errors.New("database unavailable"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusQueued)
	job := store.getJob("job-1")
	if job.LeaseOwner != "" || job.LeaseUntil != nil {
		t.Fatalf("lease not released: owner=%q until=%v", job.LeaseOwner, job.LeaseUntil)
	}
	if jobRuntime.submitCount() != 0 {
		t.Fatalf("submit calls = %d, want 0", jobRuntime.submitCount())
	}
	if store.countEvents("release:job-1") != 2 {
		// One release from the failed dispatch, one from re-claiming the same
		// still-queued job in the same cycle before the duplicate guard trips.
		t.Fatalf("release events = %d, want 2", store.countEvents("release:job-1"))
	}
}

func TestDispatchRedactsSubmitFailureMessage(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	jobRuntime := newFakeJobRuntime()
	jobRuntime.submitFunc = func(_ context.Context, _ string, _ *connectordomain.Connector) (string, error) {
		return "", Permanent(errors.New("connect failed password=hunter2 token=abc123"))
	}
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	run := store.getRun("job-1")
	if strings.Contains(run.Error, "hunter2") || strings.Contains(run.Error, "abc123") {
		t.Fatalf("run error leaks credentials: %q", run.Error)
	}
	if !strings.Contains(run.Error, "password=***") {
		t.Fatalf("run error = %q, want redacted password", run.Error)
	}
}

func TestDispatchPostSubmitConflictStopsOrphanRuntime(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	store.markRunErr = domain.ErrConflict
	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	// The fake store simulates a concurrent cancel winning the race while the
	// submission is in flight, so the job is terminal by the time the mark lands.
	requireJobStatus(t, store, "job-1", domain.StatusCancelled)
	stops := jobRuntime.stopCalls()
	if len(stops) != 1 {
		t.Fatalf("stop calls = %v, want 1 orphan stop", stops)
	}
	if stops[0].RuntimeRef != "runtime-connector-1" || stops[0].TenantID != "tenant-a" {
		t.Fatalf("stop call = %+v", stops[0])
	}
	job := store.getJob("job-1")
	if job.LeaseOwner != "" {
		t.Fatalf("lease owner = %q, want released", job.LeaseOwner)
	}
}

func TestDispatchStopsSubmittingWhenLeaseIsLost(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	submitCalls := 0
	jobRuntime := newFakeJobRuntime()
	jobRuntime.submitFunc = func(_ context.Context, _ string, _ *connectordomain.Connector) (string, error) {
		submitCalls++
		return "", Retryable(errors.New("upstream timeout"))
	}
	var sleeps int
	store.renewErr = domain.ErrConflict
	store.renewOnCall = 2
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, func(opts *DispatcherOptions) {
		opts.Sleep = func(_ context.Context, _ time.Duration) error {
			sleeps++
			return nil
		}
	})

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1 (second submit must not happen)", submitCalls)
	}
	if sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1", sleeps)
	}
	requireJobStatus(t, store, "job-1", domain.StatusQueued)
}

func TestDispatchSameConnectorConflictFailsNewerJob(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	older := newRunningJob("tenant-a", "job-old", "connector-1", "runtime-old")
	older.CreatedAt = older.CreatedAt.Add(-time.Minute)
	store.addJob(older)
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))

	jobRuntime := newFakeJobRuntime()
	dispatcher := newTestDispatcher(t, store, reader, jobRuntime, nil)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusFailed)
	run := store.getRun("job-1")
	if !strings.Contains(run.Error, "connector_sync_conflict") {
		t.Fatalf("run error = %q, want connector_sync_conflict", run.Error)
	}
	if jobRuntime.submitCount() != 0 {
		t.Fatalf("submit calls = %d, want 0", jobRuntime.submitCount())
	}
}

func TestClaimQueuedLeaseIsExclusive(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newQueuedJob("tenant-a", "job-1", "connector-1"))
	ctx := context.Background()

	claimed, err := store.ClaimQueued(ctx, "owner-a", clock.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("first ClaimQueued() error = %v", err)
	}
	if claimed.ID != "job-1" {
		t.Fatalf("claimed job = %q, want job-1", claimed.ID)
	}
	if _, err := store.ClaimQueued(ctx, "owner-b", clock.Now().Add(30*time.Second)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second ClaimQueued() error = %v, want ErrNotFound", err)
	}
	if err := store.RenewLease(ctx, "job-1", "owner-b", clock.Now().Add(time.Minute)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("RenewLease(owner-b) error = %v, want ErrConflict", err)
	}
	if err := store.RenewLease(ctx, "job-1", "owner-a", clock.Now().Add(time.Minute)); err != nil {
		t.Fatalf("RenewLease(owner-a) error = %v", err)
	}
	clock.Advance(61 * time.Second)
	if _, err := store.ClaimQueued(ctx, "owner-b", clock.Now().Add(30*time.Second)); err != nil {
		t.Fatalf("ClaimQueued() after lease expiry error = %v", err)
	}
}
