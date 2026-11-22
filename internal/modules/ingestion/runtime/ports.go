// Package runtime runs the ingestion sync loop: a dispatcher claims queued
// jobs and submits them to the connector runtime, and a reconciler observes
// running jobs until they can be reported terminal. Both are ordinary module
// goroutines cancelled through the supplied context, so the configuration is
// identical in every instance.
package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

// ErrInvalidConfig reports a missing required runtime dependency or option.
var ErrInvalidConfig = errors.New("invalid ingestion runtime configuration")

// ConnectorReader reads the connector targeted by a sync job. Its signature
// matches the connectors service.
type ConnectorReader interface {
	GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error)
}

// JobRuntime is the connector runtime control port: it submits one connector as
// a runtime resource, stops it and reports its current status. All calls are
// tenant scoped.
type JobRuntime interface {
	Submit(ctx context.Context, tenantID string, connector *connectordomain.Connector) (runtimeRef string, err error)
	Stop(ctx context.Context, tenantID, runtimeRef string) error
	Status(ctx context.Context, tenantID, runtimeRef string) (RuntimeStatus, error)
}

// RuntimeState is a normalized connector runtime state. Unknown states are kept
// distinct so the reconciler can leave a job running instead of guessing.
type RuntimeState string

const (
	StateRunning RuntimeState = "running"
	StateFailed  RuntimeState = "failed"
	StatePaused  RuntimeState = "paused"
	StateStopped RuntimeState = "stopped"
	StateMissing RuntimeState = "missing"
	StateUnknown RuntimeState = "unknown"
)

// ParseRuntimeState normalizes a raw connector runtime status string.
func ParseRuntimeState(value string) RuntimeState {
	switch normalizeState(value) {
	case "running", "run":
		return StateRunning
	case "failed", "error", "unhealthy":
		return StateFailed
	case "paused":
		return StatePaused
	case "stopped", "stop":
		return StateStopped
	case "missing", "not_found", "notfound", "deleted", "absent":
		return StateMissing
	default:
		return StateUnknown
	}
}

// RuntimeStatus is the normalized status of the runtime resource behind one
// sync job. Ready covers runtimes that report readiness separately from the
// source/sink task states.
type RuntimeStatus struct {
	Source        RuntimeState
	Sink          RuntimeState
	Ready         bool
	FailedMessage string
}

// Overall collapses the source and sink states plus readiness into one state
// with a fixed priority: failed > missing > stopped > paused > running > unknown.
func (s RuntimeStatus) Overall() RuntimeState {
	for _, candidate := range []RuntimeState{StateFailed, StateMissing, StateStopped, StatePaused} {
		if s.Source == candidate || s.Sink == candidate {
			return candidate
		}
	}
	if s.Source == StateRunning && s.Sink == StateRunning {
		return StateRunning
	}
	if s.Ready && isRunningOrUnset(s.Source) && isRunningOrUnset(s.Sink) {
		return StateRunning
	}
	return StateUnknown
}

// SnapshotVerifier checks whether the sink snapshot behind a runtime is already
// queryable and stable. done=false means the snapshot is not ready yet; the
// reconciler keeps the job running and retries.
type SnapshotVerifier interface {
	VerifySnapshot(ctx context.Context, job *domain.SyncJob, runtimeRef string) (verifiedRows int64, done bool, err error)
}

// RuntimeStore is the narrow persistence port used by the dispatcher and
// reconciler. The PostgreSQL ingestion repo implements it.
type RuntimeStore interface {
	ClaimQueued(ctx context.Context, leaseOwner string, leaseUntil time.Time) (*domain.SyncJob, error)
	RenewLease(ctx context.Context, jobID, leaseOwner string, leaseUntil time.Time) error
	ReleaseLease(ctx context.Context, jobID, leaseOwner string) error
	MarkRunningRuntime(ctx context.Context, job *domain.SyncJob, runtimeRef string, attempt int) error
	RecordAttempt(ctx context.Context, tenantID, jobID string, attempt int, message string, stats map[string]any) error
	MarkJobFailed(ctx context.Context, tenantID, jobID, message string, stats map[string]any) error
	MarkJobSucceeded(ctx context.Context, tenantID, jobID string, stats map[string]any) error
	ListRunning(ctx context.Context, limit int) ([]domain.SyncJob, error)
	SetJobRuntimeStats(ctx context.Context, tenantID, jobID string, stats map[string]any) error
	FindActiveJobByConnector(ctx context.Context, tenantID, connectorID, excludeJobID string) (*domain.SyncJob, error)
}

// Metrics receives optional low-cardinality runtime observations. Implementations
// must never label tenant, job or connector identifiers. result values are
// short outcome strings such as "success", "failed", "retry" or "error".
type Metrics interface {
	ObserveSubmit(duration time.Duration, result string)
	IncRetry(operation, result string)
	ObserveReconcile(duration time.Duration, result string)
}

func isRunningOrUnset(state RuntimeState) bool {
	return state == "" || state == StateRunning
}

func normalizeState(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
