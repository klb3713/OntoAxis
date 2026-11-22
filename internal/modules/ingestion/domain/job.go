// Package domain contains ingestion business types and the sync job/run state
// machine. It is independent of transport and persistence.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// JobStatus is the lifecycle state shared by sync jobs and sync runs.
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusSucceeded JobStatus = "succeeded"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

var (
	// ErrNotFound reports a sync job or run absent from the current tenant.
	ErrNotFound = errors.New("ingestion record not found")
	// ErrConflict reports an illegal state transition or connector state.
	ErrConflict = errors.New("ingestion conflict")
	// ErrInvalidTransition reports a state-machine violation.
	ErrInvalidTransition = errors.New("invalid sync job transition")
	// ErrInvalidStatus reports an unsupported status value.
	ErrInvalidStatus = errors.New("invalid sync job status")
)

// Terminal reports whether no further state transition is allowed.
func (s JobStatus) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

// ParseStatus validates an ingestion status value.
func ParseStatus(value string) (JobStatus, error) {
	status := JobStatus(value)
	switch status {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return status, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidStatus, value)
	}
}

// CanTransition encodes the only legal job transitions:
// queued → running → succeeded | failed, and queued | running → cancelled.
func CanTransition(from, to JobStatus) bool {
	switch from {
	case StatusQueued:
		return to == StatusRunning || to == StatusCancelled
	case StatusRunning:
		return to == StatusSucceeded || to == StatusFailed || to == StatusCancelled
	default:
		return false
	}
}

// ValidateTransition returns ErrInvalidTransition for any illegal edge.
func ValidateTransition(from, to JobStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// SyncJob is the durable task record created for a connector sync request.
type SyncJob struct {
	ID          string
	TenantID    string
	ConnectorID string
	Mode        string
	Status      JobStatus
	Message     string
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

// SyncRun is one execution attempt projection for a sync job.
type SyncRun struct {
	ID         string
	TenantID   string
	JobID      string
	Status     JobStatus
	Stats      map[string]any
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}
