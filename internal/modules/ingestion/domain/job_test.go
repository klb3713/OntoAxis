package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

func TestCanTransition(t *testing.T) {
	t.Parallel()
	legal := [][2]domain.JobStatus{
		{domain.StatusQueued, domain.StatusRunning},
		{domain.StatusQueued, domain.StatusCancelled},
		{domain.StatusRunning, domain.StatusSucceeded},
		{domain.StatusRunning, domain.StatusFailed},
		{domain.StatusRunning, domain.StatusCancelled},
	}
	for _, pair := range legal {
		if err := domain.ValidateTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("transition %s -> %s invalid: %v", pair[0], pair[1], err)
		}
	}
	illegal := [][2]domain.JobStatus{
		{domain.StatusQueued, domain.StatusSucceeded},
		{domain.StatusQueued, domain.StatusFailed},
		{domain.StatusRunning, domain.StatusQueued},
		{domain.StatusSucceeded, domain.StatusRunning},
		{domain.StatusFailed, domain.StatusCancelled},
		{domain.StatusCancelled, domain.StatusRunning},
	}
	for _, pair := range illegal {
		if err := domain.ValidateTransition(pair[0], pair[1]); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("transition %s -> %s error = %v, want ErrInvalidTransition", pair[0], pair[1], err)
		}
	}
}

func TestTerminal(t *testing.T) {
	t.Parallel()
	for _, status := range []domain.JobStatus{domain.StatusSucceeded, domain.StatusFailed, domain.StatusCancelled} {
		if !status.Terminal() {
			t.Fatalf("%s should be terminal", status)
		}
	}
	for _, status := range []domain.JobStatus{domain.StatusQueued, domain.StatusRunning} {
		if status.Terminal() {
			t.Fatalf("%s should not be terminal", status)
		}
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()
	if got, err := domain.ParseStatus("queued"); err != nil || got != domain.StatusQueued {
		t.Fatalf("ParseStatus(queued) = %q, %v", got, err)
	}
	if _, err := domain.ParseStatus("unknown"); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("ParseStatus(unknown) error = %v", err)
	}
}

func TestLeaseActive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	until := now.Add(time.Minute)
	if !(&domain.SyncJob{LeaseUntil: &until}).LeaseActive(now) {
		t.Fatal("unexpired lease should be active")
	}
	expired := now.Add(-time.Second)
	if (&domain.SyncJob{LeaseUntil: &expired}).LeaseActive(now) {
		t.Fatal("expired lease should not be active")
	}
	if (&domain.SyncJob{}).LeaseActive(now) {
		t.Fatal("lease without owner should not be active")
	}
	var nilJob *domain.SyncJob
	if nilJob.LeaseActive(now) {
		t.Fatal("nil job should not report an active lease")
	}
}

func TestRunStatsConstants(t *testing.T) {
	t.Parallel()
	keys := map[string]string{
		"phase":           domain.StatsPhase,
		"runtime_ref":     domain.StatsRuntimeRef,
		"attempt":         domain.StatsAttempt,
		"attempts":        domain.StatsAttempts,
		"last_error":      domain.StatsLastError,
		"verified_rows":   domain.StatsVerifiedRows,
		"expected_rows":   domain.StatsExpectedRows,
		"dataset_id":      domain.StatsDatasetID,
		"cleanup_pending": domain.StatsCleanupPending,
	}
	for instance, key := range keys {
		if instance != key {
			t.Fatalf("stats key %q != %q", key, instance)
		}
	}
	phases := map[string]string{
		"submitting":   domain.PhaseSubmitting,
		"snapshotting": domain.PhaseSnapshotting,
		"verifying":    domain.PhaseVerifying,
		"stopping":     domain.PhaseStopping,
		"done":         domain.PhaseDone,
	}
	for value, phase := range phases {
		if value != phase {
			t.Fatalf("phase %q != %q", phase, value)
		}
	}
}
