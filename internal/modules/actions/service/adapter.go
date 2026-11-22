package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

// ErrEngineUnavailable marks submissions that could not reach the workflow
// engine. The execution is persisted as failed with the engine_unavailable
// failure code before this error is returned, so callers may retry safely.
var ErrEngineUnavailable = errors.New("action execution engine unavailable")

// ExecutionMode identifies the execution engine backing an action submission.
type ExecutionMode string

const (
	// ExecutionModeTemporal submits executions to the Temporal workflow engine.
	ExecutionModeTemporal ExecutionMode = "temporal"
	// ExecutionModeNoop is an explicit local fallback that records intent only.
	ExecutionModeNoop ExecutionMode = "noop"
)

// ParseExecutionMode normalizes a configured execution mode. The empty string is
// treated as unspecified and rejected so callers must choose explicitly.
func ParseExecutionMode(raw string) (ExecutionMode, error) {
	switch ExecutionMode(raw) {
	case ExecutionModeTemporal:
		return ExecutionModeTemporal, nil
	case ExecutionModeNoop:
		return ExecutionModeNoop, nil
	default:
		return "", fmt.Errorf("unknown execution mode %q: must be %q or %q", raw, ExecutionModeTemporal, ExecutionModeNoop)
	}
}

// ExecutionRef identifies the engine-side run of one execution.
type ExecutionRef struct {
	ExecutionID uuid.UUID
	WorkflowID  string
	RunID       string
}

// ExecutionInput is the engine submission payload captured for one execution.
type ExecutionInput struct {
	TenantID     uuid.UUID
	ExecutionID  uuid.UUID
	ActionTypeID uuid.UUID
	ObjectID     *uuid.UUID
	Definition   domain.ExecutionDefinition
}

// ExecutionInfo is the engine-side view of an execution.
type ExecutionInfo struct {
	Status         string
	RunID          string
	FailureCode    string
	FailureMessage string
}

// AdmissionDecision is a human approval decision delivered to the workflow.
type AdmissionDecision struct {
	Allowed    bool
	ApproverID uuid.UUID
	Comment    string
	ReasonCode string
}

// Adapter is the workflow engine port. It must never execute step effects
// synchronously; it only submits, cancels, signals and describes engine runs.
type Adapter interface {
	Mode() ExecutionMode
	Start(ctx context.Context, in ExecutionInput) (ExecutionRef, error)
	Cancel(ctx context.Context, ref ExecutionRef, reason string) error
	SignalApproval(ctx context.Context, ref ExecutionRef, decision AdmissionDecision) error
	Describe(ctx context.Context, ref ExecutionRef) (ExecutionInfo, error)
	Health(ctx context.Context) error
}

// NoopAdapter satisfies Adapter without a workflow engine. It is an explicit
// degraded mode: executions are recorded but never executed. It must only be
// selected deliberately, never as a silent fallback when a real engine is
// configured.
type NoopAdapter struct{}

var _ Adapter = NoopAdapter{}

// Mode reports the explicit noop mode.
func (NoopAdapter) Mode() ExecutionMode { return ExecutionModeNoop }

// Start records the intent by returning a synthetic reference.
func (NoopAdapter) Start(_ context.Context, in ExecutionInput) (ExecutionRef, error) {
	return ExecutionRef{
		ExecutionID: in.ExecutionID,
		WorkflowID:  "noop-" + in.ExecutionID.String(),
	}, nil
}

// Cancel is a no-op for the noop adapter.
func (NoopAdapter) Cancel(context.Context, ExecutionRef, string) error { return nil }

// SignalApproval is a no-op for the noop adapter.
func (NoopAdapter) SignalApproval(context.Context, ExecutionRef, AdmissionDecision) error {
	return nil
}

// Describe reports the execution as engine-side running. The noop adapter never
// advances state, which keeps the degraded mode visible in execution detail.
func (NoopAdapter) Describe(context.Context, ExecutionRef) (ExecutionInfo, error) {
	return ExecutionInfo{Status: domain.ExecutionStatusRunning}, nil
}

// Health always succeeds because the noop adapter has no external dependency.
func (NoopAdapter) Health(context.Context) error { return nil }
