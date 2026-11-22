package domain_test

import (
	"errors"
	"testing"

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
