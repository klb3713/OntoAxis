package temporal

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	sdktemporal "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

// Workflow and activity identifiers. These strings are part of the deployment
// contract: renaming them requires draining the task queue.
const (
	WorkflowName = "ActionExecutionWorkflow"

	// SignalApproval carries a service.AdmissionDecision payload.
	SignalApproval = "action.approval"

	ActivityMarkRunning    = "MarkRunning"
	ActivityMarkStatus     = "MarkStatus"
	ActivityApplyStep      = "ApplyStep"
	ActivityCreateApproval = "CreateApproval"
	ActivityExpireApproval = "ExpireApproval"
	ActivityCompensateStep = "CompensateStep"
	ActivityMarkFinished   = "MarkFinished"

	// DefaultStepTimeout bounds a single activity attempt when the step does not
	// provide its own timeout.
	DefaultStepTimeout = 5 * time.Minute
)

// activityRef is used only to take stable method references for
// workflow.ExecuteActivity. It is never called directly.
var activityRef *Activities

// WorkflowInput is the deterministic input of an execution workflow. All
// business configuration is snapshotted at submission time.
type WorkflowInput struct {
	TenantID     uuid.UUID
	ExecutionID  uuid.UUID
	ActionTypeID uuid.UUID
	ObjectID     *uuid.UUID
	Definition   domain.ExecutionDefinition
}

type completedStep struct {
	Index int
	Step  domain.StepDefinition
}

type executionWorkflow struct {
	ctx       workflow.Context
	in        WorkflowInput
	def       domain.ExecutionDefinition
	completed []completedStep
}

// ActionExecutionWorkflow is the deterministic Temporal workflow that drives
// one action execution. It performs no I/O outside activities. State
// transitions are written by activities that call the reviewed ExecutionStore.
func ActionExecutionWorkflow(ctx workflow.Context, in WorkflowInput) (string, error) {
	w := &executionWorkflow{ctx: ctx, in: in, def: in.Definition}
	if w.def.ApprovalTimeout <= 0 {
		w.def.ApprovalTimeout = DefaultApprovalTimeout
	}
	w.ctx = workflow.WithActivityOptions(w.ctx, w.activityOptions(nil))
	return w.run()
}

func (w *executionWorkflow) run() (string, error) {
	info := workflow.GetInfo(w.ctx)
	var running MarkRunningResult
	runningInput := MarkRunningInput{
		TenantID:    w.in.TenantID,
		ExecutionID: w.in.ExecutionID,
		WorkflowID:  info.WorkflowExecution.ID,
		RunID:       info.WorkflowExecution.RunID,
	}
	if err := workflow.ExecuteActivity(w.ctx, activityRef.MarkRunning, runningInput).Get(w.ctx, &running); err != nil {
		if sdktemporal.IsCanceledError(err) {
			return w.finishWithCompensation(domain.ExecutionStatusCancelled, "cancelled", "execution cancelled before it started")
		}
		return w.finish(domain.ExecutionStatusFailed, FailureCodeActivityFailed, fmt.Sprintf("start execution: %v", err), nil)
	}
	if running.Terminal {
		// The execution was already terminal (for example cancelled while
		// queued); exit without further writes.
		return running.Status, nil
	}
	for index := range w.def.Steps {
		step := w.def.Steps[index]
		if step.RequiresApproval {
			proceed, status, err := w.awaitApproval(index, step)
			if !proceed {
				return status, err
			}
		}
		result, err := w.applyStep(index, step)
		if err != nil {
			if sdktemporal.IsCanceledError(err) {
				if !step.ReadOnly {
					// The activity may have applied its side effect before the
					// cancellation arrived; compensate conservatively.
					w.completed = append(w.completed, completedStep{Index: index, Step: step})
				}
				return w.finishWithCompensation(domain.ExecutionStatusCancelled, "cancelled", fmt.Sprintf("execution cancelled during step %q", step.ID))
			}
			if !step.ReadOnly {
				// The activity exhausted its retries. The failed attempt may
				// have applied a partial side effect, so compensate
				// conservatively instead of leaving a residual change behind.
				w.completed = append(w.completed, completedStep{Index: index, Step: step})
			}
			return w.finishWithCompensation(domain.ExecutionStatusFailed, FailureCodeActivityFailed, fmt.Sprintf("step %q: %v", step.ID, err))
		}
		if result.Status == domain.ExecutionStepStatusFailed {
			if result.FailureCode == FailureCodeExecutionTerminal {
				return w.finish(domain.ExecutionStatusCancelled, FailureCodeExecutionTerminal, result.FailureMessage, nil)
			}
			if result.SideEffectsApplied && !step.ReadOnly {
				w.completed = append(w.completed, completedStep{Index: index, Step: step})
			}
			return w.finishWithCompensation(domain.ExecutionStatusFailed, result.FailureCode, result.FailureMessage)
		}
		w.completed = append(w.completed, completedStep{Index: index, Step: step})
	}
	return w.finish(domain.ExecutionStatusSucceeded, "", "", nil)
}

// awaitApproval requests a human decision for one step and waits for the
// approval signal, the approval timer, or workflow cancellation. It returns
// proceed=false when the workflow must finish with the returned status.
func (w *executionWorkflow) awaitApproval(index int, step domain.StepDefinition) (bool, string, error) {
	stepIndex := index
	var created CreateApprovalResult
	createCtx := workflow.WithActivityOptions(w.ctx, w.activityOptions(nil))
	err := workflow.ExecuteActivity(createCtx, activityRef.CreateApproval, CreateApprovalInput{
		TenantID:    w.in.TenantID,
		ExecutionID: w.in.ExecutionID,
		StepIndex:   index,
		Timeout:     w.def.ApprovalTimeout,
	}).Get(createCtx, &created)
	if err != nil {
		if sdktemporal.IsCanceledError(err) {
			return w.stop(domain.ExecutionStatusCancelled, "cancelled", fmt.Sprintf("execution cancelled while requesting approval for step %q", step.ID))
		}
		return w.stopWithoutCompensation(domain.ExecutionStatusFailed, FailureCodeActivityFailed, fmt.Sprintf("request approval for step %q: %v", step.ID, err))
	}
	pending := domain.ApprovalStatePending
	pendingStatus := domain.ExecutionStatusWaitingApproval
	if err := w.markStatus(createCtx, MarkStatusInput{
		TenantID:         w.in.TenantID,
		ExecutionID:      w.in.ExecutionID,
		Status:           pendingStatus,
		ApprovalState:    pending,
		CurrentStepIndex: &stepIndex,
		FromStatuses:     []string{domain.ExecutionStatusRunning},
	}); err != nil {
		return w.stop(domain.ExecutionStatusFailed, FailureCodeActivityFailed, fmt.Sprintf("mark waiting approval: %v", err))
	}

	waitStarted := workflow.Now(w.ctx)
	timeout := w.def.ApprovalTimeout
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	signalCh := workflow.GetSignalChannel(w.ctx, SignalApproval)
	timer := workflow.NewTimer(w.ctx, timeout)
	selector := workflow.NewSelector(w.ctx)
	var decision service.AdmissionDecision
	received := false
	timedOut := false
	cancelled := false
	selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(w.ctx, &decision)
		received = true
	})
	selector.AddFuture(timer, func(workflow.Future) {
		timedOut = true
	})
	selector.AddReceive(w.ctx.Done(), func(workflow.ReceiveChannel, bool) {
		cancelled = true
	})
	selector.Select(w.ctx)
	waited := workflow.Now(w.ctx).Sub(waitStarted)

	if cancelled {
		return w.stop(domain.ExecutionStatusCancelled, "cancelled", fmt.Sprintf("execution cancelled while awaiting approval for step %q", step.ID))
	}
	if !cancelled && !received && timedOut {
		expireCtx := workflow.WithActivityOptions(w.ctx, w.activityOptions(nil))
		_ = workflow.ExecuteActivity(expireCtx, activityRef.ExpireApproval, ExpireApprovalInput{
			TenantID:    w.in.TenantID,
			ExecutionID: w.in.ExecutionID,
			StepIndex:   index,
		}).Get(expireCtx, nil)
		return w.stop(domain.ExecutionStatusTimedOut, repo.ApprovalReasonTimeout, fmt.Sprintf("approval for step %q timed out", step.ID))
	}
	if !decision.Allowed {
		rejected := domain.ApprovalStateRejected
		_ = w.markStatus(w.ctx, MarkStatusInput{
			TenantID:      w.in.TenantID,
			ExecutionID:   w.in.ExecutionID,
			ApprovalState: rejected,
			FailureCode:   repo.ApprovalReasonRejected,
			FromStatuses:  []string{domain.ExecutionStatusWaitingApproval},
			ApprovalWait:  waited,
		})
		return w.stop(domain.ExecutionStatusCancelled, repo.ApprovalReasonRejected, fmt.Sprintf("step %q was rejected", step.ID))
	}
	approved := domain.ApprovalStateApproved
	running := domain.ExecutionStatusRunning
	if err := w.markStatus(w.ctx, MarkStatusInput{
		TenantID:         w.in.TenantID,
		ExecutionID:      w.in.ExecutionID,
		Status:           running,
		ApprovalState:    approved,
		CurrentStepIndex: &stepIndex,
		FromStatuses:     []string{domain.ExecutionStatusWaitingApproval},
		ApprovalWait:     waited,
	}); err != nil {
		return w.stop(domain.ExecutionStatusFailed, FailureCodeActivityFailed, fmt.Sprintf("resume execution after approval: %v", err))
	}
	return true, "", nil
}

// stop finishes the workflow with compensation and reports that the run must
// not continue.
func (w *executionWorkflow) stop(status, failureCode, failureMessage string) (bool, string, error) {
	finalStatus, err := w.finishWithCompensation(status, failureCode, failureMessage)
	return false, finalStatus, err
}

// stopWithoutCompensation finishes the workflow without running compensation.
func (w *executionWorkflow) stopWithoutCompensation(status, failureCode, failureMessage string) (bool, string, error) {
	finalStatus, err := w.finish(status, failureCode, failureMessage, nil)
	return false, finalStatus, err
}

func (w *executionWorkflow) applyStep(index int, step domain.StepDefinition) (ApplyStepResult, error) {
	stepCtx := workflow.WithActivityOptions(w.ctx, w.activityOptions(&step))
	var result ApplyStepResult
	err := workflow.ExecuteActivity(stepCtx, activityRef.ApplyStep, ApplyStepInput{
		TenantID:    w.in.TenantID,
		ExecutionID: w.in.ExecutionID,
		StepIndex:   index,
		Step:        step,
	}).Get(stepCtx, &result)
	return result, err
}

func (w *executionWorkflow) markStatus(ctx workflow.Context, in MarkStatusInput) error {
	ctx = workflow.WithActivityOptions(ctx, w.activityOptions(nil))
	return workflow.ExecuteActivity(ctx, activityRef.MarkStatus, in).Get(ctx, nil)
}

// activityOptions derives deterministic activity options from the definition
// and the optional step. Retries only apply to transient failures; permanent
// effect failures are converted to failed activity results by the activities.
func (w *executionWorkflow) activityOptions(step *domain.StepDefinition) workflow.ActivityOptions {
	timeout := DefaultStepTimeout
	retry := retryPolicyFromDefinition(w.def.RetryPolicy)
	if step != nil {
		if step.Timeout > 0 {
			timeout = step.Timeout
		}
		if step.RetryPolicy != nil {
			retry = retryPolicyFromDefinition(*step.RetryPolicy)
		}
	}
	return workflow.ActivityOptions{
		StartToCloseTimeout:    timeout,
		ScheduleToCloseTimeout: timeout * 2,
		RetryPolicy:            retry,
	}
}

func retryPolicyFromDefinition(def domain.RetryPolicy) *sdktemporal.RetryPolicy {
	attempts := int32(def.MaximumAttempts)
	if attempts <= 0 {
		attempts = 3
	}
	initial := def.InitialInterval
	if initial <= 0 {
		initial = time.Second
	}
	maximum := def.MaximumInterval
	if maximum <= 0 {
		maximum = 30 * time.Second
	}
	if maximum < initial {
		maximum = initial
	}
	coefficient := def.BackoffCoefficient
	if coefficient < 1.0 {
		coefficient = 2.0
	}
	return &sdktemporal.RetryPolicy{
		InitialInterval:    initial,
		BackoffCoefficient: coefficient,
		MaximumInterval:    maximum,
		MaximumAttempts:    attempts,
	}
}

// finish writes a terminal status using the normal workflow context.
func (w *executionWorkflow) finish(status, failureCode, failureMessage string, result map[string]any) (string, error) {
	return w.markFinished(w.ctx, status, failureCode, failureMessage, result)
}

// finishWithCompensation marks the execution as compensating, runs every
// declared compensation in reverse order on a disconnected context (so a
// cancelled workflow still compensates), and writes the final status.
// Read-only steps are skipped. Residual side effects turn the final status into
// compensation_failed.
func (w *executionWorkflow) finishWithCompensation(status, failureCode, failureMessage string) (string, error) {
	if len(w.completed) == 0 {
		return w.finish(status, failureCode, failureMessage, nil)
	}
	ectx, _ := workflow.NewDisconnectedContext(w.ctx)
	ectx = workflow.WithActivityOptions(ectx, w.activityOptions(nil))
	compensating := domain.ExecutionStatusCompensating
	_ = workflow.ExecuteActivity(ectx, activityRef.MarkStatus, MarkStatusInput{
		TenantID:       w.in.TenantID,
		ExecutionID:    w.in.ExecutionID,
		Status:         compensating,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
		FromStatuses: []string{
			domain.ExecutionStatusRunning,
			domain.ExecutionStatusWaitingApproval,
		},
	}).Get(ectx, nil)

	residual := 0
	for i := len(w.completed) - 1; i >= 0; i-- {
		item := w.completed[i]
		if item.Step.ReadOnly {
			continue
		}
		if item.Step.Compensation == nil {
			if !item.Step.AllowWithoutCompensation {
				residual++
			}
			continue
		}
		var result CompensateStepResult
		err := workflow.ExecuteActivity(ectx, activityRef.CompensateStep, CompensateStepInput{
			TenantID:    w.in.TenantID,
			ExecutionID: w.in.ExecutionID,
			StepIndex:   item.Index,
			Step:        item.Step,
		}).Get(ectx, &result)
		if err != nil || !result.Compensated {
			residual++
		}
	}
	finalStatus := status
	finalCode := failureCode
	finalMessage := failureMessage
	if residual > 0 {
		finalStatus = domain.ExecutionStatusCompensationFailed
		if finalCode == "" {
			finalCode = FailureCodeActivityFailed
		}
		finalMessage = fmt.Sprintf("%s; %d step(s) could not be compensated", finalMessage, residual)
	}
	return w.markFinished(ectx, finalStatus, finalCode, finalMessage, nil)
}

// markFinished writes the terminal projection. A cancelled workflow retries the
// terminal write on a disconnected context because the workflow context can no
// longer schedule activities.
func (w *executionWorkflow) markFinished(ctx workflow.Context, status, failureCode, failureMessage string, result map[string]any) (string, error) {
	in := MarkFinishedInput{
		TenantID:       w.in.TenantID,
		ExecutionID:    w.in.ExecutionID,
		Status:         status,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
		Result:         result,
	}
	ctx = workflow.WithActivityOptions(ctx, w.activityOptions(nil))
	err := workflow.ExecuteActivity(ctx, activityRef.MarkFinished, in).Get(ctx, nil)
	if err != nil && sdktemporal.IsCanceledError(err) {
		disconnected, _ := workflow.NewDisconnectedContext(w.ctx)
		ctx = workflow.WithActivityOptions(disconnected, w.activityOptions(nil))
		err = workflow.ExecuteActivity(ctx, activityRef.MarkFinished, in).Get(ctx, nil)
	}
	if err != nil {
		return "", fmt.Errorf("mark execution %s: %w", status, err)
	}
	return status, nil
}
