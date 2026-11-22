package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("action not found")
	ErrInvalidInput = errors.New("invalid action input")
	ErrConflict     = errors.New("action conflict")
)

const (
	ActionStatusActive   = "active"
	ActionStatusDraft    = "draft"
	ActionStatusDisabled = "disabled"

	ExecutionStatusQueued    = "queued"
	ExecutionStatusRunning   = "running"
	ExecutionStatusSucceeded = "succeeded"
	ExecutionStatusFailed    = "failed"
	ExecutionStatusCancelled = "cancelled"

	// ExecutionStatusWaitingApproval means the workflow is paused on a step
	// that requires a human decision.
	ExecutionStatusWaitingApproval = "waiting_approval"
	// ExecutionStatusCompensating means completed steps are being rolled back.
	ExecutionStatusCompensating = "compensating"
	// ExecutionStatusTimedOut means the execution exceeded its workflow or
	// approval deadline.
	ExecutionStatusTimedOut = "timed_out"
	// ExecutionStatusCompensationFailed is a terminal state that keeps the
	// residual side effects visible; it must never be reported as success.
	ExecutionStatusCompensationFailed = "compensation_failed"

	// ExecutionStepStatusPending marks a step that has not started yet.
	ExecutionStepStatusPending = "pending"
	// ExecutionStepStatusRunning marks a step whose activity is in flight.
	ExecutionStepStatusRunning = "running"
	// ExecutionStepStatusSucceeded marks a step whose side effect was applied.
	ExecutionStepStatusSucceeded = "succeeded"
	// ExecutionStepStatusFailed marks a step that exhausted its retries.
	ExecutionStepStatusFailed = "failed"
	// ExecutionStepStatusCompensated marks a step whose side effect was rolled back.
	ExecutionStepStatusCompensated = "compensated"
	// ExecutionStepStatusCompensationFailed marks a step whose rollback failed.
	ExecutionStepStatusCompensationFailed = "compensation_failed"

	// ApprovalStateNone means the execution has no approval in flight.
	ApprovalStateNone = "none"
	// ApprovalStatePending means an approval task is waiting for a decision.
	ApprovalStatePending = "pending"
	// ApprovalStateApproved means the pending approval was granted.
	ApprovalStateApproved = "approved"
	// ApprovalStateRejected means the pending approval was denied.
	ApprovalStateRejected = "rejected"
	// ApprovalStateExpired means the approval deadline passed undecided.
	ApprovalStateExpired = "expired"

	// ApprovalTaskStatusPending marks an approval task awaiting a decision.
	ApprovalTaskStatusPending = ApprovalStatePending
	// ApprovalTaskStatusApproved marks an approval task that was granted.
	ApprovalTaskStatusApproved = ApprovalStateApproved
	// ApprovalTaskStatusRejected marks an approval task that was denied.
	ApprovalTaskStatusRejected = ApprovalStateRejected
	// ApprovalTaskStatusExpired marks an approval task whose deadline passed.
	ApprovalTaskStatusExpired = ApprovalStateExpired
)

type ActionType struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	ObjectTypeID *uuid.UUID     `json:"object_type_id,omitempty"`
	Parameters   map[string]any `json:"parameters"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type Execution struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	ActionTypeID uuid.UUID      `json:"action_type_id"`
	Status       string         `json:"status"`
	Parameters   map[string]any `json:"parameters"`
	Result       map[string]any `json:"result,omitempty"`
	Error        string         `json:"error,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`

	// ObjectID is the optional business object the execution acts on.
	ObjectID *uuid.UUID `json:"object_id,omitempty"`
	// DefinitionSnapshot is the execution definition captured at submission
	// time; later Action Type edits must not affect submitted executions.
	DefinitionSnapshot map[string]any `json:"definition_snapshot,omitempty"`
	// WorkflowID and WorkflowRunID reference the external workflow engine run.
	WorkflowID    string `json:"workflow_id,omitempty"`
	WorkflowRunID string `json:"workflow_run_id,omitempty"`
	// Attempt is the number of engine-level attempts started for this execution.
	Attempt int `json:"attempt"`
	// CurrentStepIndex is the zero-based index of the step being processed.
	CurrentStepIndex int `json:"current_step_index"`
	// ApprovalState is one of the ApprovalState* constants.
	ApprovalState string `json:"approval_state"`
	// FailureCode and FailureMessage carry a redacted, stable failure reason.
	FailureCode    string `json:"failure_code,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
	// CancelRequestedAt records when cancellation was requested, if any.
	CancelRequestedAt *time.Time `json:"cancel_requested_at,omitempty"`
}

// ExecutionStep is one row of the per-execution step ledger. The unique
// (execution_id, step_index) pair provides the activity idempotency boundary.
type ExecutionStep struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	ExecutionID    uuid.UUID      `json:"execution_id"`
	StepIndex      int            `json:"step_index"`
	ActivityType   string         `json:"activity_type"`
	Status         string         `json:"status"`
	Attempt        int            `json:"attempt"`
	InputHash      string         `json:"input_hash"`
	BeforeSnapshot map[string]any `json:"before_snapshot,omitempty"`
	AfterSnapshot  map[string]any `json:"after_snapshot,omitempty"`
	Error          string         `json:"error,omitempty"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ApprovalTask is a human decision request for one execution step.
type ApprovalTask struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	ExecutionID uuid.UUID  `json:"execution_id"`
	StepIndex   int        `json:"step_index"`
	Status      string     `json:"status"`
	RequestedAt time.Time  `json:"requested_at"`
	DeadlineAt  time.Time  `json:"deadline_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	DecidedBy   *uuid.UUID `json:"decided_by,omitempty"`
	Comment     string     `json:"comment,omitempty"`
	ReasonCode  string     `json:"reason_code,omitempty"`
}

// IdempotencyRecord stores the first response produced for a
// (tenant_id, key) request pair so replays return the same execution.
type IdempotencyRecord struct {
	TenantID    uuid.UUID      `json:"tenant_id"`
	Key         string         `json:"key"`
	RequestHash string         `json:"request_hash"`
	ExecutionID uuid.UUID      `json:"execution_id"`
	Response    map[string]any `json:"response,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// executionTransitions lists the legal execution status edges of design D6.
// Terminal statuses are absent, so no edge can leave them.
var executionTransitions = map[string][]string{
	ExecutionStatusQueued: {
		ExecutionStatusRunning,
		ExecutionStatusCancelled,
		ExecutionStatusWaitingApproval,
	},
	ExecutionStatusRunning: {
		ExecutionStatusSucceeded,
		ExecutionStatusFailed,
		ExecutionStatusCancelled,
		ExecutionStatusTimedOut,
		ExecutionStatusWaitingApproval,
		ExecutionStatusCompensating,
	},
	ExecutionStatusWaitingApproval: {
		ExecutionStatusRunning,
		ExecutionStatusCancelled,
		ExecutionStatusTimedOut,
		ExecutionStatusCompensating,
	},
	ExecutionStatusCompensating: {
		ExecutionStatusFailed,
		ExecutionStatusCancelled,
		ExecutionStatusTimedOut,
		ExecutionStatusCompensationFailed,
	},
}

var terminalExecutionStatuses = map[string]struct{}{
	ExecutionStatusSucceeded:          {},
	ExecutionStatusFailed:             {},
	ExecutionStatusCancelled:          {},
	ExecutionStatusTimedOut:           {},
	ExecutionStatusCompensationFailed: {},
}

// CanTransition reports whether the execution status may move from one status
// to the next. Unknown statuses and no-op transitions are rejected.
func CanTransition(from, to string) bool {
	if from == "" || to == "" || from == to {
		return false
	}
	for _, allowed := range executionTransitions[strings.TrimSpace(from)] {
		if allowed == strings.TrimSpace(to) {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the status is a final execution status that can no
// longer change.
func IsTerminal(status string) bool {
	_, ok := terminalExecutionStatuses[strings.TrimSpace(status)]
	return ok
}

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidInput
	}
	return nil
}

func NormalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", ActionStatusActive:
		return ActionStatusActive
	case ActionStatusDraft:
		return ActionStatusDraft
	case ActionStatusDisabled:
		return ActionStatusDisabled
	default:
		return ""
	}
}
