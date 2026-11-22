package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

func newTestReconciler(t *testing.T, store *fakeStore, reader ConnectorReader, jobRuntime JobRuntime, verifier SnapshotVerifier, mutate func(*ReconcilerOptions)) *Reconciler {
	t.Helper()
	opts := ReconcilerOptions{
		Interval:       time.Hour,
		BatchSize:      10,
		StableInterval: 5 * time.Second,
		StabilityTTL:   time.Minute,
		Clock:          store.clock.Now,
		Logger:         discardLogger(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	reconciler, err := NewReconciler(store, reader, jobRuntime, verifier, opts)
	if err != nil {
		t.Fatalf("NewReconciler() error = %v", err)
	}
	return reconciler
}

func TestReconcileMapsRuntimeStatusToFailed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		status      RuntimeStatus
		wantMessage string
	}{
		{
			name:        "failed with message",
			status:      RuntimeStatus{Source: StateRunning, Sink: StateFailed, FailedMessage: "sink crashed"},
			wantMessage: "runtime_failed: sink crashed",
		},
		{
			name:        "failed without message",
			status:      RuntimeStatus{Source: StateRunning, Sink: StateFailed},
			wantMessage: "runtime_failed: the connector runtime reported a failure",
		},
		{
			name:        "missing",
			status:      RuntimeStatus{Source: StateRunning, Sink: StateMissing},
			wantMessage: "runtime_not_running: runtime_missing",
		},
		{
			name:        "stopped",
			status:      RuntimeStatus{Source: StateRunning, Sink: StateStopped},
			wantMessage: "runtime_not_running: runtime_stopped",
		},
		{
			name:        "paused",
			status:      RuntimeStatus{Source: StateRunning, Sink: StatePaused},
			wantMessage: "runtime_not_running: runtime_paused",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(newFakeClock())
			store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
			jobRuntime := newFakeJobRuntime()
			jobRuntime.statusFunc = func(context.Context, string, string) (RuntimeStatus, error) {
				return tc.status, nil
			}
			verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
			reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

			if err := reconciler.ReconcileOnce(context.Background()); err != nil {
				t.Fatalf("ReconcileOnce() error = %v", err)
			}
			requireJobStatus(t, store, "job-1", domain.StatusFailed)
			run := store.getRun("job-1")
			if run.Error != tc.wantMessage {
				t.Fatalf("run error = %q, want %q", run.Error, tc.wantMessage)
			}
			if verifier.callCount() != 0 {
				t.Fatalf("verifier calls = %d, want 0", verifier.callCount())
			}
			if len(jobRuntime.stopCalls()) != 0 {
				t.Fatalf("stop calls = %d, want 0", len(jobRuntime.stopCalls()))
			}
		})
	}
}

func TestReconcileRedactsRuntimeFailureMessage(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	jobRuntime.statusFunc = func(context.Context, string, string) (RuntimeStatus, error) {
		return RuntimeStatus{Source: StateRunning, Sink: StateFailed, FailedMessage: "auth failed token=abc123"}, nil
	}
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, nil, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	run := store.getRun("job-1")
	if strings.Contains(run.Error, "abc123") {
		t.Fatalf("run error leaks secret: %q", run.Error)
	}
	if !strings.Contains(run.Error, "token=***") {
		t.Fatalf("run error = %q, want redacted token", run.Error)
	}
}

func TestReconcileKeepsRunningForUnknownRuntimeState(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	jobRuntime.statusFunc = func(context.Context, string, string) (RuntimeStatus, error) {
		return RuntimeStatus{Source: StateRunning, Sink: StateUnknown}, nil
	}
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	for i := 0; i < 2; i++ {
		if err := reconciler.ReconcileOnce(context.Background()); err != nil {
			t.Fatalf("ReconcileOnce() error = %v", err)
		}
		store.clock.Advance(5 * time.Second)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
	if verifier.callCount() != 0 {
		t.Fatalf("verifier calls = %d, want 0", verifier.callCount())
	}
}

func TestReconcileSuccessRequiresTwoStableSamples(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-1"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, reader, jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)

	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusSucceeded)

	run := store.getRun("job-1")
	if run.Stats[domain.StatsPhase] != domain.PhaseDone {
		t.Fatalf("phase = %v, want done", run.Stats[domain.StatsPhase])
	}
	if run.Stats[domain.StatsVerifiedRows] != int64(10) {
		t.Fatalf("verified_rows = %v, want 10", run.Stats[domain.StatsVerifiedRows])
	}
	if run.Stats[domain.StatsDatasetID] != "dataset-connector-1" {
		t.Fatalf("dataset_id = %v, want dataset-connector-1", run.Stats[domain.StatsDatasetID])
	}
	if run.Stats[domain.StatsRuntimeRef] != "runtime-1" {
		t.Fatalf("runtime_ref = %v, want runtime-1", run.Stats[domain.StatsRuntimeRef])
	}
	stops := jobRuntime.stopCalls()
	if len(stops) != 1 {
		t.Fatalf("stop calls = %d, want 1", len(stops))
	}
	if stops[0].TenantID != "tenant-a" || stops[0].RuntimeRef != "runtime-1" {
		t.Fatalf("stop call = %+v", stops[0])
	}
}

func TestReconcileSecondSampleTooEarlyDoesNotSucceed(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	clock.Advance(2 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)

	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusSucceeded)
}

func TestReconcileUnstableRowsKeepJobRunning(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(
		verifyResult{rows: 10, done: true},
		verifyResult{rows: 11, done: true},
		verifyResult{rows: 11, done: true},
	)
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)

	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusSucceeded)
	if rows := store.getRun("job-1").Stats[domain.StatsVerifiedRows]; rows != int64(11) {
		t.Fatalf("verified_rows = %v, want 11", rows)
	}
}

func TestReconcileSnapshotNotDoneKeepsRunning(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: false})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	for i := 0; i < 2; i++ {
		if err := reconciler.ReconcileOnce(context.Background()); err != nil {
			t.Fatalf("ReconcileOnce() error = %v", err)
		}
		clock.Advance(5 * time.Second)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
	if len(jobRuntime.stopCalls()) != 0 {
		t.Fatalf("stop calls = %d, want 0", len(jobRuntime.stopCalls()))
	}
}

func TestReconcileStatusErrorKeepsJobRunning(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	jobRuntime.statusFunc = func(context.Context, string, string) (RuntimeStatus, error) {
		return RuntimeStatus{}, errors.New("connector runtime unavailable")
	}
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, nil, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
}

func TestReconcileVerifierErrorKeepsJobRunning(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{err: errors.New("snapshot read failed")})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
}

func TestReconcileCleanupFailureRecordsCleanupPending(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	jobRuntime.stopFunc = func(context.Context, string, string) error {
		return errors.New("runtime API unavailable")
	}
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusSucceeded)
	run := store.getRun("job-1")
	if run.Status != domain.StatusSucceeded {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
	if run.Stats[domain.StatsCleanupPending] != true {
		t.Fatalf("cleanup_pending = %v, want true", run.Stats[domain.StatsCleanupPending])
	}
	if !strings.Contains(run.Stats[domain.StatsLastError].(string), "runtime API unavailable") {
		t.Fatalf("last_error = %v", run.Stats[domain.StatsLastError])
	}
}

func TestReconcileTerminalConflictDoesNotOverrideJob(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	store.succeedErr = domain.ErrConflict
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() should tolerate a terminal-conflict, got %v", err)
	}
	if len(jobRuntime.stopCalls()) != 0 {
		t.Fatalf("stop calls = %d, want 0", len(jobRuntime.stopCalls()))
	}
}

func TestReconcileSkipsJobWithoutRuntimeRef(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", ""))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
	if len(jobRuntime.statusCalls()) != 0 {
		t.Fatalf("status calls = %d, want 0", len(jobRuntime.statusCalls()))
	}
}

func TestReconcileWithoutVerifierKeepsJobRunning(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-1", "connector-1", "runtime-1"))
	jobRuntime := newFakeJobRuntime()
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), jobRuntime, nil, nil)

	for i := 0; i < 2; i++ {
		if err := reconciler.ReconcileOnce(context.Background()); err != nil {
			t.Fatalf("ReconcileOnce() error = %v", err)
		}
		clock.Advance(5 * time.Second)
	}
	requireJobStatus(t, store, "job-1", domain.StatusRunning)
}

func TestReconcileUsesJobTenant(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	store := newFakeStore(clock)
	store.addJob(newRunningJob("tenant-a", "job-a", "connector-a", "runtime-a"))
	store.addJob(newRunningJob("tenant-b", "job-b", "connector-b", "runtime-b"))
	reader := newFakeConnectorReader()
	reader.add(newActiveConnector("tenant-a", "connector-a"))
	reader.add(newActiveConnector("tenant-b", "connector-b"))
	jobRuntime := newFakeJobRuntime()
	verifier := newFakeVerifier(verifyResult{rows: 10, done: true})
	reconciler := newTestReconciler(t, store, reader, jobRuntime, verifier, nil)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	clock.Advance(5 * time.Second)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}

	want := map[string]string{"tenant-a": "runtime-a", "tenant-b": "runtime-b"}
	calls := jobRuntime.statusCalls()
	// Two cycles, two running jobs: each cycle checks both runtimes.
	if len(calls) != 4 {
		t.Fatalf("status calls = %d, want 4", len(calls))
	}
	for _, call := range calls {
		ref, ok := want[call.TenantID]
		if !ok {
			t.Fatalf("status called with unexpected tenant %q", call.TenantID)
		}
		if call.RuntimeRef != ref {
			t.Fatalf("status tenant %q runtime ref = %q, want %q", call.TenantID, call.RuntimeRef, ref)
		}
	}
	requireJobStatus(t, store, "job-a", domain.StatusSucceeded)
	requireJobStatus(t, store, "job-b", domain.StatusSucceeded)
}

func TestReconcileBatchAndListError(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	store.listErr = errors.New("database unavailable")
	reconciler := newTestReconciler(t, store, newFakeConnectorReader(), newFakeJobRuntime(), nil, nil)

	err := reconciler.ReconcileOnce(context.Background())
	if err == nil {
		t.Fatal("ReconcileOnce() error = nil, want list error")
	}
	if !errors.Is(err, store.listErr) {
		t.Fatalf("ReconcileOnce() error = %v, want wrapped list error", err)
	}
}
