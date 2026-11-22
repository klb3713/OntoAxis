package temporal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

// Failure codes persisted on executions and steps.
const (
	FailureCodeEngineUnavailable  = "engine_unavailable"
	FailureCodeExecutionTerminal  = "execution_terminal"
	FailureCodeActivityFailed     = "activity_failed"
	FailureCodeInvalidInput       = "invalid_input"
	FailureCodeNotFound           = "not_found"
	FailureCodeConflict           = "conflict"
	FailureCodeResidualSideEffect = "residual_side_effects"
)

// AuditEvent is the worker-side audit projection. Audit sinks are injected by
// deployment wiring; a nil sink disables emission.
type AuditEvent struct {
	Name         string
	TenantID     uuid.UUID
	ExecutionID  uuid.UUID
	StepIndex    int
	Status       string
	FailureCode  string
	FailureCause string
	At           time.Time
}

// Activity input and output contracts. They are JSON serializable and shared
// with the workflow.

type MarkRunningInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	WorkflowID  string
	RunID       string
}

type MarkRunningResult struct {
	Terminal bool
	Status   string
	Attempt  int
}

type MarkStatusInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	// Status is an optional target execution status.
	Status string
	// ApprovalState is an optional target approval state.
	ApprovalState  string
	FailureCode    string
	FailureMessage string
	// CurrentStepIndex updates the execution progress when non-nil.
	CurrentStepIndex *int
	// FromStatuses constrains the CAS; empty means the reviewed runtime
	// progress statuses.
	FromStatuses []string
	// ApprovalWait is recorded as an approval waiting duration when positive.
	ApprovalWait time.Duration
}

type ApplyStepInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	StepIndex   int
	Step        domain.StepDefinition
}

type ApplyStepResult struct {
	Status             string
	SideEffectsApplied bool
	Before             map[string]any
	After              map[string]any
	FailureCode        string
	FailureMessage     string
}

type CreateApprovalInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	StepIndex   int
	Timeout     time.Duration
}

type CreateApprovalResult struct {
	TaskID     uuid.UUID
	DeadlineAt time.Time
}

type ExpireApprovalInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	StepIndex   int
}

type CompensateStepInput struct {
	TenantID    uuid.UUID
	ExecutionID uuid.UUID
	StepIndex   int
	Step        domain.StepDefinition
}

type CompensateStepResult struct {
	Compensated    bool
	FailureMessage string
}

type MarkFinishedInput struct {
	TenantID       uuid.UUID
	ExecutionID    uuid.UUID
	Status         string
	FailureCode    string
	FailureMessage string
	Result         map[string]any
}

// Activities implements the state-writing activities of the execution
// workflow. Every method is tenant scoped, idempotent enough to survive worker
// crashes, and never touches business objects directly: side effects go
// through the injected EffectApplier.
type Activities struct {
	Store   ExecutionStore
	Effects EffectApplier
	// Now is injectable for tests; it defaults to time.Now.
	Now func() time.Time
	// Audit receives audit projections when non-nil.
	Audit func(ctx context.Context, event AuditEvent)
	// Metrics receives counters when non-nil.
	Metrics MetricsRecorder
}

// NewActivities builds the activity set with the reviewed defaults.
func NewActivities(store ExecutionStore, effects EffectApplier) *Activities {
	if effects == nil {
		effects = NoopEffectApplier{}
	}
	return &Activities{Store: store, Effects: effects}
}

func (a *Activities) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

func (a *Activities) effects() EffectApplier {
	if a.Effects == nil {
		return NoopEffectApplier{}
	}
	return a.Effects
}

func (a *Activities) audit(ctx context.Context, event AuditEvent) {
	if a.Audit == nil {
		return
	}
	if event.At.IsZero() {
		event.At = a.now()
	}
	a.Audit(ctx, event)
}

// recordAttempt emits an activity retry metric when the Temporal attempt number
// is greater than one.
func (a *Activities) recordAttempt(ctx context.Context, name string) {
	if a.Metrics == nil {
		return
	}
	if info := activity.GetInfo(ctx); info.Attempt > 1 {
		a.Metrics.ActivityRetried(name)
	}
}

// MarkRunning transitions the execution to running and stores the engine
// reference. It is idempotent: an already running execution is accepted, and a
// terminal execution is reported back so the workflow can exit without writes.
func (a *Activities) MarkRunning(ctx context.Context, in MarkRunningInput) (MarkRunningResult, error) {
	ctx, span := workflowSpan(ctx)
	defer span.End()
	a.recordAttempt(ctx, "MarkRunning")
	execution, err := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if err != nil {
		return MarkRunningResult{}, fmt.Errorf("load execution: %w", err)
	}
	if domain.IsTerminal(execution.Status) {
		return MarkRunningResult{Terminal: true, Status: execution.Status}, nil
	}
	if execution.Status == domain.ExecutionStatusRunning {
		attempt := execution.Attempt
		if attempt == 0 {
			attempt = 1
		}
		return MarkRunningResult{Status: execution.Status, Attempt: attempt}, nil
	}
	started := a.now()
	status := domain.ExecutionStatusRunning
	attempt := execution.Attempt + 1
	patch := repo.ExecutionPatch{
		Status:    &status,
		Attempt:   &attempt,
		StartedAt: &started,
	}
	if in.WorkflowID != "" {
		workflowID := in.WorkflowID
		patch.WorkflowID = &workflowID
	}
	if in.RunID != "" {
		runID := in.RunID
		patch.WorkflowRunID = &runID
	}
	updated, err := a.Store.UpdateExecutionRuntime(ctx, in.TenantID, in.ExecutionID, patch,
		[]string{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			current, getErr := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
			if getErr == nil {
				if domain.IsTerminal(current.Status) {
					return MarkRunningResult{Terminal: true, Status: current.Status}, nil
				}
				if current.Status == domain.ExecutionStatusRunning {
					return MarkRunningResult{Status: current.Status, Attempt: current.Attempt}, nil
				}
			}
		}
		return MarkRunningResult{}, fmt.Errorf("mark execution running: %w", err)
	}
	a.audit(ctx, AuditEvent{Name: "action.execution.started", TenantID: in.TenantID, ExecutionID: in.ExecutionID, Status: updated.Status})
	return MarkRunningResult{Status: updated.Status, Attempt: updated.Attempt}, nil
}

// MarkStatus applies an optional status and/or approval-state projection with a
// status CAS. Conflicts are tolerated when the target state is already reached,
// which keeps workflow replays idempotent.
func (a *Activities) MarkStatus(ctx context.Context, in MarkStatusInput) error {
	ctx, span := actionActivitySpan(ctx, "MarkStatus")
	defer span.End()
	execution, err := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if err != nil {
		return fmt.Errorf("load execution: %w", err)
	}
	if domain.IsTerminal(execution.Status) {
		return nil
	}
	patch := repo.ExecutionPatch{}
	if in.Status != "" {
		status := in.Status
		if execution.Status != status && !domain.CanTransition(execution.Status, status) {
			return fmt.Errorf("mark status: %w: cannot transition %s to %s", domain.ErrConflict, execution.Status, status)
		}
		patch.Status = &status
	}
	if in.ApprovalState != "" {
		approvalState := in.ApprovalState
		patch.ApprovalState = &approvalState
	}
	if in.FailureCode != "" {
		code := in.FailureCode
		patch.FailureCode = &code
	}
	if in.FailureMessage != "" {
		message := sanitizeErrorText(in.FailureMessage)
		patch.FailureMessage = &message
	}
	if in.CurrentStepIndex != nil {
		index := *in.CurrentStepIndex
		patch.CurrentStepIndex = &index
	}
	if patch.Status == nil && patch.ApprovalState == nil && patch.FailureCode == nil && patch.FailureMessage == nil && patch.CurrentStepIndex == nil {
		return nil
	}
	fromStatuses := in.FromStatuses
	if len(fromStatuses) == 0 {
		fromStatuses = []string{
			domain.ExecutionStatusQueued,
			domain.ExecutionStatusRunning,
			domain.ExecutionStatusWaitingApproval,
			domain.ExecutionStatusCompensating,
		}
	}
	updated, err := a.Store.UpdateExecutionRuntime(ctx, in.TenantID, in.ExecutionID, patch, fromStatuses)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			current, getErr := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
			if getErr == nil {
				if domain.IsTerminal(current.Status) {
					return nil
				}
				if in.Status == "" || current.Status == in.Status {
					return nil
				}
			}
		}
		return fmt.Errorf("mark execution status: %w", err)
	}
	if in.ApprovalWait > 0 && a.Metrics != nil {
		a.Metrics.ApprovalWait(in.ApprovalWait)
	}
	if updated.Status == domain.ExecutionStatusRunning && in.ApprovalState == domain.ApprovalStateApproved {
		a.audit(ctx, AuditEvent{Name: "action.approval.approved", TenantID: in.TenantID, ExecutionID: in.ExecutionID, Status: updated.Status})
	}
	if in.ApprovalState == domain.ApprovalStateRejected {
		a.audit(ctx, AuditEvent{Name: "action.approval.rejected", TenantID: in.TenantID, ExecutionID: in.ExecutionID, FailureCode: in.FailureCode})
	}
	return nil
}

// ApplyStep applies one step effect and records the step ledger. A permanent
// failure is returned as a failed result (no activity retry); transient
// failures are returned as errors so the Temporal retry policy applies.
func (a *Activities) ApplyStep(ctx context.Context, in ApplyStepInput) (ApplyStepResult, error) {
	ctx, span := actionActivitySpan(ctx, "ApplyStep")
	defer span.End()
	a.recordAttempt(ctx, "ApplyStep")
	execution, err := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if err != nil {
		return ApplyStepResult{}, fmt.Errorf("load execution: %w", err)
	}
	if domain.IsTerminal(execution.Status) {
		return ApplyStepResult{
			Status:         domain.ExecutionStepStatusFailed,
			FailureCode:    FailureCodeExecutionTerminal,
			FailureMessage: "execution is already " + execution.Status,
		}, nil
	}
	existing, loadErr := a.Store.GetExecutionStep(ctx, in.TenantID, in.ExecutionID, in.StepIndex)
	if loadErr != nil && !errors.Is(loadErr, domain.ErrNotFound) {
		return ApplyStepResult{}, fmt.Errorf("load execution step: %w", loadErr)
	}
	if loadErr == nil && existing.Status == domain.ExecutionStepStatusSucceeded {
		return ApplyStepResult{
			Status:             domain.ExecutionStepStatusSucceeded,
			SideEffectsApplied: true,
			Before:             existing.BeforeSnapshot,
			After:              existing.AfterSnapshot,
		}, nil
	}
	attempt := 0
	if loadErr == nil {
		attempt = existing.Attempt
	}
	now := a.now()
	ledger := domain.ExecutionStep{
		ID:           uuid.New(),
		TenantID:     in.TenantID,
		ExecutionID:  in.ExecutionID,
		StepIndex:    in.StepIndex,
		ActivityType: in.Step.ActivityType,
		Status:       domain.ExecutionStepStatusRunning,
		Attempt:      attempt + 1,
		InputHash:    hashStep(in.Step),
		StartedAt:    &now,
	}
	if _, err := a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger); err != nil {
		return ApplyStepResult{}, fmt.Errorf("record step start: %w", err)
	}
	params := mergeParameters(execution.Parameters, in.Step.Parameters)
	before, after, applyErr := a.effects().Apply(ctx, in.TenantID, in.ExecutionID, in.Step, params)
	if applyErr != nil {
		finished := a.now()
		ledger.Status = domain.ExecutionStepStatusFailed
		ledger.Error = sanitizeErrorText(applyErr.Error())
		ledger.FinishedAt = &finished
		_, _ = a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger)
		if isPermanentEffectError(applyErr) {
			code := failureCodeFor(applyErr)
			message := sanitizeErrorText(applyErr.Error())
			a.audit(ctx, AuditEvent{Name: "action.step.failed", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, Status: domain.ExecutionStepStatusFailed, FailureCode: code, FailureCause: message})
			return ApplyStepResult{
				Status:             domain.ExecutionStepStatusFailed,
				SideEffectsApplied: !in.Step.ReadOnly,
				FailureCode:        code,
				FailureMessage:     message,
			}, nil
		}
		return ApplyStepResult{}, fmt.Errorf("apply step %d: %w", in.StepIndex, applyErr)
	}
	finished := a.now()
	ledger.Status = domain.ExecutionStepStatusSucceeded
	ledger.BeforeSnapshot = before
	ledger.AfterSnapshot = after
	ledger.FinishedAt = &finished
	stored, err := a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger)
	if err != nil {
		return ApplyStepResult{}, fmt.Errorf("record step success: %w", err)
	}
	index := in.StepIndex
	_ = a.MarkStatus(ctx, MarkStatusInput{
		TenantID:         in.TenantID,
		ExecutionID:      in.ExecutionID,
		CurrentStepIndex: &index,
		FromStatuses:     []string{domain.ExecutionStatusRunning, domain.ExecutionStatusWaitingApproval},
	})
	a.audit(ctx, AuditEvent{Name: "action.step.succeeded", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, Status: domain.ExecutionStepStatusSucceeded})
	return ApplyStepResult{
		Status:             domain.ExecutionStepStatusSucceeded,
		SideEffectsApplied: true,
		Before:             stored.BeforeSnapshot,
		After:              stored.AfterSnapshot,
	}, nil
}

// CreateApproval records a pending approval task for one step. A duplicate
// (activity retry or replay) is treated as success.
func (a *Activities) CreateApproval(ctx context.Context, in CreateApprovalInput) (CreateApprovalResult, error) {
	ctx, span := actionActivitySpan(ctx, "CreateApproval")
	defer span.End()
	a.recordAttempt(ctx, "CreateApproval")
	execution, err := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if err != nil {
		return CreateApprovalResult{}, fmt.Errorf("load execution: %w", err)
	}
	if domain.IsTerminal(execution.Status) {
		return CreateApprovalResult{}, fmt.Errorf("create approval: %w: execution is %s", domain.ErrConflict, execution.Status)
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	now := a.now()
	task := domain.ApprovalTask{
		ID:          uuid.New(),
		TenantID:    in.TenantID,
		ExecutionID: in.ExecutionID,
		StepIndex:   in.StepIndex,
		Status:      domain.ApprovalTaskStatusPending,
		RequestedAt: now,
		DeadlineAt:  now.Add(timeout),
	}
	created, err := a.Store.CreateApprovalTask(ctx, in.TenantID, task)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return CreateApprovalResult{DeadlineAt: task.DeadlineAt}, nil
		}
		return CreateApprovalResult{}, fmt.Errorf("create approval task: %w", err)
	}
	a.audit(ctx, AuditEvent{Name: "action.approval.requested", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, Status: domain.ApprovalTaskStatusPending})
	return CreateApprovalResult{TaskID: created.ID, DeadlineAt: created.DeadlineAt}, nil
}

// ExpireApproval expires overdue approval tasks and projects the expired
// approval state on the execution. It is idempotent.
func (a *Activities) ExpireApproval(ctx context.Context, in ExpireApprovalInput) error {
	ctx, span := actionActivitySpan(ctx, "ExpireApproval")
	defer span.End()
	now := a.now()
	if _, err := a.Store.ExpireApprovalTasks(ctx, now); err != nil {
		return fmt.Errorf("expire approval tasks: %w", err)
	}
	state := domain.ApprovalStateExpired
	if err := a.MarkStatus(ctx, MarkStatusInput{
		TenantID:      in.TenantID,
		ExecutionID:   in.ExecutionID,
		ApprovalState: state,
		FailureCode:   repo.ApprovalReasonTimeout,
		FromStatuses:  []string{domain.ExecutionStatusWaitingApproval},
	}); err != nil {
		return err
	}
	a.audit(ctx, AuditEvent{Name: "action.approval.timeout", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, FailureCode: repo.ApprovalReasonTimeout})
	return nil
}

// CompensateStep rolls back the side effect of one succeeded step using its
// declared compensation. Permanent compensation failures are recorded as a
// failed result so the workflow can collect every residual effect.
func (a *Activities) CompensateStep(ctx context.Context, in CompensateStepInput) (CompensateStepResult, error) {
	ctx, span := actionActivitySpan(ctx, "CompensateStep")
	defer span.End()
	a.recordAttempt(ctx, "CompensateStep")
	if in.Step.Compensation == nil {
		return CompensateStepResult{Compensated: false, FailureMessage: FailureCodeResidualSideEffect}, nil
	}
	existing, loadErr := a.Store.GetExecutionStep(ctx, in.TenantID, in.ExecutionID, in.StepIndex)
	if loadErr != nil && !errors.Is(loadErr, domain.ErrNotFound) {
		return CompensateStepResult{}, fmt.Errorf("load execution step: %w", loadErr)
	}
	if loadErr == nil {
		switch existing.Status {
		case domain.ExecutionStepStatusCompensated:
			return CompensateStepResult{Compensated: true}, nil
		case domain.ExecutionStepStatusCompensationFailed:
			return CompensateStepResult{Compensated: false, FailureMessage: existing.Error}, nil
		}
	}
	attempt := 0
	var before, after map[string]any
	if loadErr == nil {
		attempt = existing.Attempt
		before = existing.BeforeSnapshot
		after = existing.AfterSnapshot
	}
	compensation := in.Step.Compensation
	execution, execErr := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if execErr != nil {
		return CompensateStepResult{}, fmt.Errorf("load execution: %w", execErr)
	}
	compensationStep := domain.StepDefinition{
		ID:           in.Step.ID,
		ActivityType: compensation.ActivityType,
		Parameters:   mergeParameters(execution.Parameters, mergeParameters(in.Step.Parameters, compensation.Parameters)),
	}
	now := a.now()
	ledger := domain.ExecutionStep{
		ID:             uuid.New(),
		TenantID:       in.TenantID,
		ExecutionID:    in.ExecutionID,
		StepIndex:      in.StepIndex,
		ActivityType:   in.Step.ActivityType,
		Status:         domain.ExecutionStepStatusRunning,
		Attempt:        attempt + 1,
		InputHash:      hashStep(compensationStep),
		BeforeSnapshot: before,
		AfterSnapshot:  after,
		StartedAt:      &now,
	}
	if _, err := a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger); err != nil {
		return CompensateStepResult{}, fmt.Errorf("record compensation start: %w", err)
	}
	_, _, compensateErr := a.effects().Apply(ctx, in.TenantID, in.ExecutionID, compensationStep, compensationStep.Parameters)
	finished := a.now()
	if compensateErr != nil {
		if isPermanentEffectError(compensateErr) {
			ledger.Status = domain.ExecutionStepStatusCompensationFailed
			ledger.Error = sanitizeErrorText(compensateErr.Error())
			ledger.FinishedAt = &finished
			_, _ = a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger)
			message := sanitizeErrorText(compensateErr.Error())
			a.audit(ctx, AuditEvent{Name: "action.step.compensation_failed", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, FailureCode: failureCodeFor(compensateErr), FailureCause: message})
			return CompensateStepResult{Compensated: false, FailureMessage: message}, nil
		}
		return CompensateStepResult{}, fmt.Errorf("compensate step %d: %w", in.StepIndex, compensateErr)
	}
	ledger.Status = domain.ExecutionStepStatusCompensated
	ledger.Error = ""
	ledger.FinishedAt = &finished
	if _, err := a.Store.UpsertExecutionStep(ctx, in.TenantID, ledger); err != nil {
		return CompensateStepResult{}, fmt.Errorf("record compensation success: %w", err)
	}
	a.audit(ctx, AuditEvent{Name: "action.step.compensated", TenantID: in.TenantID, ExecutionID: in.ExecutionID, StepIndex: in.StepIndex, Status: domain.ExecutionStepStatusCompensated})
	return CompensateStepResult{Compensated: true}, nil
}

// MarkFinished writes the terminal status projection. Terminal rows are never
// overwritten, so a cancel that raced the workflow keeps its terminal status.
func (a *Activities) MarkFinished(ctx context.Context, in MarkFinishedInput) error {
	ctx, span := actionActivitySpan(ctx, "MarkFinished")
	defer span.End()
	if in.Status == "" {
		return fmt.Errorf("mark finished: %w: status is required", domain.ErrInvalidInput)
	}
	execution, err := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
	if err != nil {
		return fmt.Errorf("load execution: %w", err)
	}
	if domain.IsTerminal(execution.Status) {
		return nil
	}
	status := in.Status
	patch := repo.ExecutionPatch{Status: &status}
	finished := a.now()
	patch.FinishedAt = &finished
	if in.FailureCode != "" {
		code := in.FailureCode
		patch.FailureCode = &code
	}
	if in.FailureMessage != "" {
		message := sanitizeErrorText(in.FailureMessage)
		patch.FailureMessage = &message
	}
	if in.Result != nil {
		patch.Result = in.Result
	}
	_, err = a.Store.UpdateExecutionRuntime(ctx, in.TenantID, in.ExecutionID, patch, []string{
		domain.ExecutionStatusQueued,
		domain.ExecutionStatusRunning,
		domain.ExecutionStatusWaitingApproval,
		domain.ExecutionStatusCompensating,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			current, getErr := a.Store.GetExecution(ctx, in.TenantID, in.ExecutionID)
			if getErr == nil && domain.IsTerminal(current.Status) {
				return nil
			}
		}
		return fmt.Errorf("mark execution finished: %w", err)
	}
	eventName := "action.execution.finished"
	switch status {
	case domain.ExecutionStatusSucceeded:
		eventName = "action.execution.succeeded"
	case domain.ExecutionStatusFailed:
		eventName = "action.execution.failed"
	case domain.ExecutionStatusCancelled:
		eventName = "action.execution.cancelled"
	case domain.ExecutionStatusTimedOut:
		eventName = "action.execution.timed_out"
	case domain.ExecutionStatusCompensationFailed:
		eventName = "action.execution.compensation_failed"
	}
	a.audit(ctx, AuditEvent{Name: eventName, TenantID: in.TenantID, ExecutionID: in.ExecutionID, Status: status, FailureCode: in.FailureCode})
	if a.Metrics != nil {
		a.Metrics.ExecutionFinished(status)
	}
	return nil
}

func mergeParameters(base, override map[string]any) map[string]any {
	size := len(base) + len(override)
	if size == 0 {
		return nil
	}
	merged := make(map[string]any, size)
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func hashStep(step domain.StepDefinition) string {
	payload, err := json.Marshal(step)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// isPermanentEffectError reports whether an effect error must not be retried.
func isPermanentEffectError(err error) bool {
	return errors.Is(err, domain.ErrInvalidInput) ||
		errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrConflict) ||
		errors.Is(err, domain.ErrInvalidDefinition)
}

func failureCodeFor(err error) string {
	switch {
	case errors.Is(err, domain.ErrInvalidInput), errors.Is(err, domain.ErrInvalidDefinition):
		return FailureCodeInvalidInput
	case errors.Is(err, domain.ErrNotFound):
		return FailureCodeNotFound
	case errors.Is(err, domain.ErrConflict):
		return FailureCodeConflict
	default:
		return FailureCodeActivityFailed
	}
}

// sanitizeErrorText keeps failure messages free of newlines and bounds them.
// Callers must never put secrets or engine text into step errors.
func sanitizeErrorText(message string) string {
	message = strings.TrimSpace(message)
	message = strings.Join(strings.Fields(message), " ")
	const maxLen = 512
	if len(message) > maxLen {
		message = message[:maxLen]
	}
	return message
}
