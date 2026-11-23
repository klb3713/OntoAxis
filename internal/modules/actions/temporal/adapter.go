package temporal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

// WorkflowIDPrefix is the deterministic workflow id prefix used for action
// executions.
const WorkflowIDPrefix = "action-execution-"

// WorkflowID derives the Temporal workflow id for one execution.
func WorkflowID(executionID uuid.UUID) string {
	return WorkflowIDPrefix + executionID.String()
}

// Adapter is the Temporal implementation of service.Adapter. It only submits,
// cancels, signals and describes executions; steps run inside the worker.
type Adapter struct {
	cfg    Config
	client client.Client
}

// Dial creates a Temporal client for the reviewed configuration. In noop mode
// there is no engine to dial and a nil client is returned.
func Dial(cfg Config) (client.Client, error) {
	if cfg.Mode == service.ExecutionModeNoop {
		return nil, nil
	}
	if cfg.HostPort == "" {
		return nil, fmt.Errorf("%w: temporal host port is required", service.ErrEngineUnavailable)
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}
	c, err := client.Dial(client.Options{HostPort: cfg.HostPort, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("%w: dial temporal: %v", service.ErrEngineUnavailable, err)
	}
	return c, nil
}

// NewAdapter dials Temporal and returns a ready execution adapter. Temporal
// mode without a reachable host fails closed.
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.Mode != service.ExecutionModeTemporal {
		return nil, fmt.Errorf("%w: temporal adapter requires execution mode %q", service.ErrEngineUnavailable, service.ExecutionModeTemporal)
	}
	c, err := Dial(cfg)
	if err != nil {
		return nil, err
	}
	return &Adapter{cfg: cfg, client: c}, nil
}

// NewAdapterWithClient builds an adapter around an existing client. It is used
// by tests and by deployments that manage the client lifecycle elsewhere.
func NewAdapterWithClient(cfg Config, c client.Client) *Adapter {
	return &Adapter{cfg: cfg, client: c}
}

// Client exposes the underlying Temporal client (worker wiring and health
// checks).
func (a *Adapter) Client() client.Client {
	return a.client
}

// Mode reports the adapter execution mode.
func (a *Adapter) Mode() service.ExecutionMode {
	return service.ExecutionModeTemporal
}

// Start submits the execution workflow. Workflow ids are deterministic
// (action-execution-<id>) and reuse is rejected, which makes a retried submit
// idempotent instead of starting a second run.
func (a *Adapter) Start(ctx context.Context, in service.ExecutionInput) (service.ExecutionRef, error) {
	if a == nil || a.client == nil {
		return service.ExecutionRef{}, fmt.Errorf("%w: temporal client is not configured", service.ErrEngineUnavailable)
	}
	options := client.StartWorkflowOptions{
		ID:                                       WorkflowID(in.ExecutionID),
		TaskQueue:                                a.cfg.TaskQueue,
		WorkflowExecutionTimeout:                 a.cfg.WorkflowRunTimeout,
		WorkflowRunTimeout:                       a.cfg.WorkflowRunTimeout,
		WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}
	if in.Definition.WorkflowTimeout > 0 {
		options.WorkflowExecutionTimeout = in.Definition.WorkflowTimeout
		options.WorkflowRunTimeout = in.Definition.WorkflowTimeout
	}
	run, err := a.client.ExecuteWorkflow(ctx, options, WorkflowName, WorkflowInput{
		TenantID:     in.TenantID,
		ExecutionID:  in.ExecutionID,
		ActionTypeID: in.ActionTypeID,
		ObjectID:     in.ObjectID,
		Definition:   in.Definition,
	})
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			// The submission already happened (for example the service CAS on
			// workflow id failed and it retried); report the existing run.
			existing, describeErr := a.describeRun(ctx, WorkflowID(in.ExecutionID))
			if describeErr == nil {
				return service.ExecutionRef{
					ExecutionID: in.ExecutionID,
					WorkflowID:  WorkflowID(in.ExecutionID),
					RunID:       existing,
				}, nil
			}
		}
		return service.ExecutionRef{}, fmt.Errorf("%w: start action workflow: %v", service.ErrEngineUnavailable, err)
	}
	return service.ExecutionRef{
		ExecutionID: in.ExecutionID,
		WorkflowID:  run.GetID(),
		RunID:       run.GetRunID(),
	}, nil
}

// Cancel requests cancellation of a running workflow. The workflow compensates
// completed steps before it finishes cancelled.
func (a *Adapter) Cancel(ctx context.Context, ref service.ExecutionRef, reason string) error {
	if err := a.validateRef(ref); err != nil {
		return err
	}
	if err := a.client.CancelWorkflow(ctx, ref.WorkflowID, ref.RunID); err != nil {
		return fmt.Errorf("cancel action workflow: %w", err)
	}
	return nil
}

// SignalApproval delivers a human approval decision to the workflow.
func (a *Adapter) SignalApproval(ctx context.Context, ref service.ExecutionRef, decision service.AdmissionDecision) error {
	if err := a.validateRef(ref); err != nil {
		return err
	}
	if err := a.client.SignalWorkflow(ctx, ref.WorkflowID, ref.RunID, SignalApproval, decision); err != nil {
		return fmt.Errorf("signal action workflow: %w", err)
	}
	return nil
}

// Describe maps the Temporal workflow status to an execution projection.
// A workflow that is no longer running is reported as terminal so the service
// never treats it as healthy progress.
func (a *Adapter) Describe(ctx context.Context, ref service.ExecutionRef) (service.ExecutionInfo, error) {
	if err := a.validateRef(ref); err != nil {
		return service.ExecutionInfo{}, err
	}
	description, err := a.client.DescribeWorkflow(ctx, ref.WorkflowID, ref.RunID)
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return service.ExecutionInfo{
				Status:         domain.ExecutionStatusFailed,
				FailureCode:    "workflow_not_found",
				FailureMessage: "workflow not found",
			}, nil
		}
		return service.ExecutionInfo{}, fmt.Errorf("describe action workflow: %w", err)
	}
	runID := ref.RunID
	status := domain.ExecutionStatusFailed
	if description != nil && description.RawResponse != nil {
		if info := description.RawResponse.WorkflowExecutionInfo; info != nil {
			if info.Execution != nil {
				runID = info.Execution.RunId
			}
			status = mapWorkflowStatus(info.Status)
		}
	}
	return service.ExecutionInfo{Status: status, RunID: runID}, nil
}

// Health verifies the Temporal frontend responds.
func (a *Adapter) Health(ctx context.Context) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("%w: temporal client is not configured", service.ErrEngineUnavailable)
	}
	if _, err := a.client.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		return fmt.Errorf("%w: temporal health check: %v", service.ErrEngineUnavailable, err)
	}
	return nil
}

// Close releases the Temporal client connection.
func (a *Adapter) Close() {
	if a != nil && a.client != nil {
		a.client.Close()
	}
}

func (a *Adapter) validateRef(ref service.ExecutionRef) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("%w: temporal client is not configured", service.ErrEngineUnavailable)
	}
	if ref.WorkflowID == "" {
		return fmt.Errorf("%w: workflow id is required", domain.ErrInvalidInput)
	}
	return nil
}

// describeRun returns the current run id of a workflow.
func (a *Adapter) describeRun(ctx context.Context, workflowID string) (string, error) {
	description, err := a.client.DescribeWorkflow(ctx, workflowID, "")
	if err != nil {
		return "", fmt.Errorf("describe workflow %q: %w", workflowID, err)
	}
	if description == nil || description.RawResponse == nil ||
		description.RawResponse.WorkflowExecutionInfo == nil ||
		description.RawResponse.WorkflowExecutionInfo.Execution == nil {
		return "", fmt.Errorf("describe workflow %q: missing execution info", workflowID)
	}
	return description.RawResponse.WorkflowExecutionInfo.Execution.RunId, nil
}

func mapWorkflowStatus(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return domain.ExecutionStatusRunning
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return domain.ExecutionStatusSucceeded
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return domain.ExecutionStatusCancelled
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return domain.ExecutionStatusTimedOut
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return domain.ExecutionStatusFailed
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return domain.ExecutionStatusFailed
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return domain.ExecutionStatusRunning
	default:
		return domain.ExecutionStatusFailed
	}
}
