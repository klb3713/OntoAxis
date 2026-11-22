package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

type Service interface {
	CreateActionType(ctx context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error)
	GetActionType(ctx context.Context, tenantID, id uuid.UUID) (domain.ActionType, error)
	ListActionTypes(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.ActionType, int64, error)
	UpdateActionType(ctx context.Context, tenantID, id uuid.UUID, display string, parameters map[string]any) (domain.ActionType, error)
	DeleteActionType(ctx context.Context, tenantID, id uuid.UUID) error
	Validate(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (bool, []string, error)
	Preview(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (map[string]any, error)
	Run(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (domain.Execution, error)
	GetExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error)
	ListExecutions(ctx context.Context, tenantID uuid.UUID, actionTypeID *uuid.UUID, limit, offset int) ([]domain.Execution, int64, error)
	CancelExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error)
	RetryExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error)
	ListApprovalTasks(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]domain.ApprovalTask, int64, error)
	GetApprovalTask(ctx context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error)
	DecideApprovalTask(ctx context.Context, tenantID, id uuid.UUID, approved bool, actorID uuid.UUID, comment string) (domain.ApprovalTask, error)
}

// ObjectTypeValidator checks that an action's target object type exists.
type ObjectTypeValidator interface {
	ObjectTypeExists(ctx context.Context, tenantID, objectTypeID string) (bool, error)
}

// Option customizes the service graph.
type Option func(*impl)

// WithObjectTypeValidator injects the ontology boundary validator.
func WithObjectTypeValidator(v ObjectTypeValidator) Option {
	return func(s *impl) { s.objectTypeValidator = v }
}

// WithAdapter overrides the execution engine port. It is a convenience for
// wiring code that cannot pass the adapter positionally; the positional
// argument still wins when this option is not used.
func WithAdapter(a Adapter) Option {
	return func(s *impl) {
		if a != nil {
			s.adapter = a
			s.mode = a.Mode()
		}
	}
}

type impl struct {
	store               repo.Store
	runtime             repo.RuntimeStore
	adapter             Adapter
	mode                ExecutionMode
	objectTypeValidator ObjectTypeValidator
}

// New builds the actions service. When the store also implements
// repo.RuntimeStore, execution definitions, workflow references, approval
// tasks and ledger rows are persisted; otherwise the Stage 1 execution flow is
// used unchanged. A nil adapter selects the explicit noop mode.
func New(store repo.Store, adapter Adapter, opts ...Option) Service {
	if adapter == nil {
		adapter = NoopAdapter{}
	}
	s := &impl{store: store, adapter: adapter, mode: adapter.Mode()}
	if runtime, ok := store.(repo.RuntimeStore); ok {
		s.runtime = runtime
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *impl) CreateActionType(ctx context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error) {
	if err := domain.ValidateName(name); err != nil {
		return domain.ActionType{}, err
	}
	if strings.TrimSpace(display) == "" {
		display = name
	}
	if status == "" {
		status = domain.ActionStatusActive
	}
	status = domain.NormalizeStatus(status)
	if status == "" {
		return domain.ActionType{}, domain.ErrInvalidInput
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	if objectTypeID != nil && s.objectTypeValidator != nil {
		exists, err := s.objectTypeValidator.ObjectTypeExists(ctx, tenantID.String(), objectTypeID.String())
		if err != nil {
			return domain.ActionType{}, err
		}
		if !exists {
			return domain.ActionType{}, domain.ErrInvalidInput
		}
	}
	return s.store.CreateActionType(ctx, tenantID, name, display, description, objectTypeID, parameters, status)
}

func (s *impl) GetActionType(ctx context.Context, tenantID, id uuid.UUID) (domain.ActionType, error) {
	return s.store.GetActionType(ctx, tenantID, id)
}

func (s *impl) ListActionTypes(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.ActionType, int64, error) {
	limit, offset = normalizePage(limit, offset)
	return s.store.ListActionTypes(ctx, tenantID, limit, offset)
}

func (s *impl) UpdateActionType(ctx context.Context, tenantID, id uuid.UUID, display string, parameters map[string]any) (domain.ActionType, error) {
	if strings.TrimSpace(display) == "" {
		return domain.ActionType{}, domain.ErrInvalidInput
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	return s.store.UpdateActionType(ctx, tenantID, id, display, parameters)
}

func (s *impl) DeleteActionType(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.store.DeleteActionType(ctx, tenantID, id)
}

func (s *impl) Validate(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (bool, []string, error) {
	actionType, err := s.store.GetActionType(ctx, tenantID, actionTypeID)
	if err != nil {
		return false, nil, err
	}
	if actionType.Status != domain.ActionStatusActive {
		return false, []string{"action type is not active"}, nil
	}
	var problems []string
	if required, ok := actionType.Parameters["required"].([]any); ok {
		for _, raw := range required {
			key, _ := raw.(string)
			if key == "" {
				continue
			}
			if _, exists := parameters[key]; !exists {
				problems = append(problems, fmt.Sprintf("missing required parameter %q", key))
			}
		}
	}
	return len(problems) == 0, problems, nil
}

func (s *impl) Preview(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (map[string]any, error) {
	actionType, err := s.store.GetActionType(ctx, tenantID, actionTypeID)
	if err != nil {
		return nil, err
	}
	valid, problems, err := s.Validate(ctx, tenantID, actionTypeID, parameters)
	if err != nil {
		return nil, err
	}
	def, defErr := resolveDefinition(actionType, parameters)
	if defErr != nil {
		valid = false
		problems = append(problems, defErr.Error())
	}
	preview := map[string]any{
		"valid":          valid,
		"errors":         problems,
		"action_type_id": actionTypeID.String(),
		"parameters":     parameters,
		"adapter":        string(s.adapter.Mode()),
		"mode":           string(s.adapter.Mode()),
	}
	if defErr == nil {
		preview["steps"] = stepSummaries(def)
		preview["approval_required"] = definitionRequiresApproval(def)
		preview["execution_definition"] = def
		preview["retry_policy"] = def.RetryPolicy
		if def.ApprovalTimeout > 0 {
			preview["approval_timeout"] = def.ApprovalTimeout.String()
		}
		if def.WorkflowTimeout > 0 {
			preview["workflow_timeout"] = def.WorkflowTimeout.String()
		}
	}
	return preview, nil
}

func (s *impl) Run(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (domain.Execution, error) {
	valid, problems, err := s.Validate(ctx, tenantID, actionTypeID, parameters)
	if err != nil {
		return domain.Execution{}, err
	}
	if !valid {
		return domain.Execution{}, fmt.Errorf("%w: %s", domain.ErrInvalidInput, strings.Join(problems, "; "))
	}
	actionType, err := s.store.GetActionType(ctx, tenantID, actionTypeID)
	if err != nil {
		return domain.Execution{}, err
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	def, err := resolveDefinition(actionType, parameters)
	if err != nil {
		return domain.Execution{}, err
	}
	objectID, err := parseObjectID(parameters)
	if err != nil {
		return domain.Execution{}, err
	}

	input := ExecutionInput{
		TenantID:     tenantID,
		ActionTypeID: actionTypeID,
		ObjectID:     objectID,
		Definition:   def,
	}

	key := idempotencyKeyFromContext(ctx)
	var requestHash string
	if key != "" && s.runtime != nil {
		requestHash = idempotencyRequestHash(actionTypeID, parameters)
		if existing, getErr := s.runtime.GetIdempotency(ctx, tenantID, key); getErr == nil {
			if existing.RequestHash != requestHash {
				return domain.Execution{}, fmt.Errorf("%w: idempotency key reused with a different request", domain.ErrConflict)
			}
			return s.store.GetExecution(ctx, tenantID, existing.ExecutionID)
		} else if !errors.Is(getErr, domain.ErrNotFound) {
			return domain.Execution{}, getErr
		}
	}

	var execution domain.Execution
	if s.runtime != nil {
		snapshot, snapshotErr := definitionSnapshot(def)
		if snapshotErr != nil {
			return domain.Execution{}, fmt.Errorf("%w: encode execution definition: %v", domain.ErrInvalidInput, snapshotErr)
		}
		execution, err = s.runtime.CreateExecutionWithDefinition(ctx, tenantID, actionTypeID, objectID, parameters, snapshot)
	} else {
		execution, err = s.store.CreateExecution(ctx, tenantID, actionTypeID, parameters, domain.ExecutionStatusQueued)
	}
	if err != nil {
		return domain.Execution{}, err
	}
	input.ExecutionID = execution.ID

	if key != "" && s.runtime != nil {
		if _, putErr := s.runtime.PutIdempotency(ctx, tenantID, key, requestHash, execution.ID, map[string]any{"execution_id": execution.ID.String()}); putErr != nil {
			if errors.Is(putErr, domain.ErrConflict) {
				existing, getErr := s.runtime.GetIdempotency(ctx, tenantID, key)
				if getErr == nil {
					status := domain.ExecutionStatusFailed
					code := "duplicate_submission"
					finished := time.Now().UTC()
					_, _ = s.runtime.UpdateExecutionRuntime(ctx, tenantID, execution.ID,
						repo.ExecutionPatch{Status: &status, FailureCode: &code, FinishedAt: &finished},
						[]string{domain.ExecutionStatusQueued})
					return s.store.GetExecution(ctx, tenantID, existing.ExecutionID)
				}
			}
			return domain.Execution{}, putErr
		}
	}

	ref, startErr := s.adapter.Start(ctx, input)
	if startErr != nil {
		s.recordStartFailure(ctx, tenantID, execution.ID, startErr)
		return domain.Execution{}, fmt.Errorf("%w: %w", ErrEngineUnavailable, startErr)
	}
	if s.runtime == nil || ref.WorkflowID == "" {
		return execution, nil
	}

	workflowID, runID := ref.WorkflowID, ref.RunID
	patch := repo.ExecutionPatch{WorkflowID: &workflowID, WorkflowRunID: &runID}
	updated, err := s.runtime.UpdateExecutionRuntime(ctx, tenantID, execution.ID, patch,
		[]string{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning, domain.ExecutionStatusWaitingApproval})
	if err != nil {
		// The workflow may have moved the execution to a terminal status before
		// the reference could be persisted; report the current row instead of
		// failing a submission that was accepted by the engine.
		if errors.Is(err, domain.ErrConflict) {
			if current, getErr := s.store.GetExecution(ctx, tenantID, execution.ID); getErr == nil {
				return current, nil
			}
		}
		return domain.Execution{}, fmt.Errorf("persist workflow reference: %w", err)
	}
	return updated, nil
}

func (s *impl) recordStartFailure(ctx context.Context, tenantID, executionID uuid.UUID, startErr error) {
	message := sanitizeFailureMessage(startErr)
	if s.runtime != nil {
		status := domain.ExecutionStatusFailed
		code := "engine_unavailable"
		finished := time.Now().UTC()
		patch := repo.ExecutionPatch{
			Status:         &status,
			FailureCode:    &code,
			FailureMessage: &message,
			FinishedAt:     &finished,
		}
		_, _ = s.runtime.UpdateExecutionRuntime(ctx, tenantID, executionID, patch, []string{domain.ExecutionStatusQueued})
		return
	}
	_, _ = s.store.UpdateExecutionStatus(ctx, tenantID, executionID, domain.ExecutionStatusFailed, message)
}

func (s *impl) GetExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	return s.store.GetExecution(ctx, tenantID, id)
}

func (s *impl) ListExecutions(ctx context.Context, tenantID uuid.UUID, actionTypeID *uuid.UUID, limit, offset int) ([]domain.Execution, int64, error) {
	limit, offset = normalizePage(limit, offset)
	return s.store.ListExecutions(ctx, tenantID, actionTypeID, limit, offset)
}

func (s *impl) CancelExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	execution, err := s.store.GetExecution(ctx, tenantID, id)
	if err != nil {
		return domain.Execution{}, err
	}
	if domain.IsTerminal(execution.Status) {
		return domain.Execution{}, domain.ErrInvalidInput
	}
	if s.runtime != nil && execution.WorkflowID != "" {
		ref := ExecutionRef{
			ExecutionID: execution.ID,
			WorkflowID:  execution.WorkflowID,
			RunID:       execution.WorkflowRunID,
		}
		if err := s.adapter.Cancel(ctx, ref, "cancelled by user"); err != nil {
			return domain.Execution{}, fmt.Errorf("cancel execution: %w", err)
		}
	}
	if s.runtime != nil {
		status := domain.ExecutionStatusCancelled
		code := "cancelled"
		finished := time.Now().UTC()
		patch := repo.ExecutionPatch{Status: &status, FailureCode: &code, FinishedAt: &finished}
		return s.runtime.UpdateExecutionRuntime(ctx, tenantID, id, patch,
			[]string{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning, domain.ExecutionStatusWaitingApproval})
	}
	return s.store.UpdateExecutionStatus(ctx, tenantID, id, domain.ExecutionStatusCancelled, "")
}

func (s *impl) RetryExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	execution, err := s.store.GetExecution(ctx, tenantID, id)
	if err != nil {
		return domain.Execution{}, err
	}
	if execution.Status != domain.ExecutionStatusFailed && execution.Status != domain.ExecutionStatusCancelled {
		return domain.Execution{}, domain.ErrInvalidInput
	}
	return s.Run(ctx, tenantID, execution.ActionTypeID, execution.Parameters)
}

func (s *impl) ListApprovalTasks(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]domain.ApprovalTask, int64, error) {
	if s.runtime == nil {
		return nil, 0, domain.ErrConflict
	}
	limit, offset = normalizePage(limit, offset)
	return s.runtime.ListApprovalTasks(ctx, tenantID, status, limit, offset)
}

func (s *impl) GetApprovalTask(ctx context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error) {
	if s.runtime == nil {
		return domain.ApprovalTask{}, domain.ErrConflict
	}
	return s.runtime.GetApprovalTask(ctx, tenantID, id)
}

func (s *impl) DecideApprovalTask(ctx context.Context, tenantID, id uuid.UUID, approved bool, actorID uuid.UUID, comment string) (domain.ApprovalTask, error) {
	if s.runtime == nil {
		return domain.ApprovalTask{}, domain.ErrConflict
	}
	if _, err := s.runtime.GetApprovalTask(ctx, tenantID, id); err != nil {
		return domain.ApprovalTask{}, err
	}
	decision := "reject"
	if approved {
		decision = "approve"
	}
	decided, err := s.runtime.DecideApprovalTask(ctx, tenantID, id, decision, actorID.String(), comment)
	if err != nil {
		return domain.ApprovalTask{}, err
	}
	execution, err := s.store.GetExecution(ctx, tenantID, decided.ExecutionID)
	if err != nil {
		return decided, err
	}
	if execution.WorkflowID == "" {
		return decided, nil
	}
	admission := AdmissionDecision{
		Allowed:    approved,
		ApproverID: actorID,
		Comment:    comment,
	}
	if !approved {
		admission.ReasonCode = repo.ApprovalReasonRejected
	}
	ref := ExecutionRef{
		ExecutionID: execution.ID,
		WorkflowID:  execution.WorkflowID,
		RunID:       execution.WorkflowRunID,
	}
	if err := s.adapter.SignalApproval(ctx, ref, admission); err != nil {
		// The decision is already persisted; a retry of the decision returns a
		// conflict and the workflow keeps waiting until its approval deadline.
		return decided, fmt.Errorf("signal approval decision: %w", err)
	}
	return decided, nil
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// defaultExecutionDefinition is the backward-compatible no-op definition used
// when an action type does not declare an execution_definition.
func defaultExecutionDefinition() domain.ExecutionDefinition {
	return domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{{
			ID:           "noop",
			ActivityType: domain.ActivityTypeNoop,
			ReadOnly:     true,
		}},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}
}

// resolveDefinition reads, parses and validates the execution definition
// declared on an action type. The returned definition has defaults applied.
func resolveDefinition(actionType domain.ActionType, _ map[string]any) (domain.ExecutionDefinition, error) {
	raw, ok := actionType.Parameters["execution_definition"]
	if !ok || raw == nil {
		return defaultExecutionDefinition(), nil
	}
	src, err := definitionSource(raw)
	if err != nil {
		return domain.ExecutionDefinition{}, err
	}
	def, err := domain.ParseExecutionDefinition(src)
	if err != nil {
		return domain.ExecutionDefinition{}, fmt.Errorf("%w: %w", domain.ErrInvalidInput, err)
	}
	if err := domain.ValidateExecutionDefinition(def); err != nil {
		return domain.ExecutionDefinition{}, fmt.Errorf("%w: %w", domain.ErrInvalidInput, err)
	}
	return def, nil
}

func definitionSource(raw any) (map[string]any, error) {
	switch value := raw.(type) {
	case map[string]any:
		return value, nil
	case string:
		var src map[string]any
		if err := json.Unmarshal([]byte(value), &src); err != nil {
			return nil, fmt.Errorf("%w: execution_definition must be a JSON object: %v", domain.ErrInvalidInput, err)
		}
		return src, nil
	default:
		return nil, fmt.Errorf("%w: execution_definition must be an object", domain.ErrInvalidInput)
	}
}

func definitionSnapshot(def domain.ExecutionDefinition) (map[string]any, error) {
	payload, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func parseObjectID(parameters map[string]any) (*uuid.UUID, error) {
	raw, ok := parameters["object_id"]
	if !ok || raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, nil
		}
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%w: object_id must be a UUID", domain.ErrInvalidInput)
		}
		return &id, nil
	case uuid.UUID:
		id := value
		return &id, nil
	default:
		return nil, fmt.Errorf("%w: object_id must be a UUID string", domain.ErrInvalidInput)
	}
}

func stepSummaries(def domain.ExecutionDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(def.Steps))
	for _, step := range def.Steps {
		out = append(out, map[string]any{
			"id":                step.ID,
			"activity_type":     step.ActivityType,
			"requires_approval": step.RequiresApproval,
			"read_only":         step.ReadOnly,
		})
	}
	return out
}

func definitionRequiresApproval(def domain.ExecutionDefinition) bool {
	for _, step := range def.Steps {
		if step.RequiresApproval {
			return true
		}
	}
	return false
}

func sanitizeFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	message = strings.Join(strings.Fields(message), " ")
	const maxLen = 512
	if len(message) > maxLen {
		message = message[:maxLen]
	}
	return message
}
