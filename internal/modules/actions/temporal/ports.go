package temporal

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

// ExecutionStore is the persistence port used by workflow activities. It is
// satisfied by *repo.PostgresStore: the runtime methods are declared by
// repo.RuntimeStore and GetExecution by repo.Store.
type ExecutionStore interface {
	GetExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error)
	UpdateExecutionRuntime(ctx context.Context, tenantID, id uuid.UUID, patch repo.ExecutionPatch, fromStatuses []string) (domain.Execution, error)
	GetExecutionStep(ctx context.Context, tenantID, executionID uuid.UUID, stepIndex int) (domain.ExecutionStep, error)
	UpsertExecutionStep(ctx context.Context, tenantID uuid.UUID, step domain.ExecutionStep) (domain.ExecutionStep, error)
	CreateApprovalTask(ctx context.Context, tenantID uuid.UUID, task domain.ApprovalTask) (domain.ApprovalTask, error)
	ExpireApprovalTasks(ctx context.Context, now time.Time) (int64, error)
}

// EffectApplier applies and compensates the business side effect of one step.
// Implementations must be idempotent for a given execution and step: activity
// retries and workflow replays can call them more than once.
type EffectApplier interface {
	Apply(ctx context.Context, tenantID, executionID uuid.UUID, step domain.StepDefinition, params map[string]any) (before, after map[string]any, err error)
}

// NoopEffectApplier records the step without touching any business object. It
// is the safe default until the object write pipeline is wired.
type NoopEffectApplier struct{}

var _ EffectApplier = NoopEffectApplier{}

// Apply returns empty snapshots and never fails.
func (NoopEffectApplier) Apply(context.Context, uuid.UUID, uuid.UUID, domain.StepDefinition, map[string]any) (map[string]any, map[string]any, error) {
	return nil, nil, nil
}

// MetricsRecorder receives worker-side counters and histograms. It is optional;
// a nil recorder disables metric emission.
type MetricsRecorder interface {
	ExecutionFinished(status string)
	ApprovalWait(time.Duration)
	ActivityRetried(activity string)
}
