package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

// Adapter is the Stage 1 Temporal/Kestra seam. The default adapter records
// intent only; a real workflow engine replaces it in Stage 2.
type Adapter interface {
	Start(ctx context.Context, actionTypeID, executionID uuid.UUID) error
}

type NoopAdapter struct{}

func (NoopAdapter) Start(context.Context, uuid.UUID, uuid.UUID) error { return nil }

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

type impl struct {
	store               repo.Store
	adapter             Adapter
	objectTypeValidator ObjectTypeValidator
}

func New(store repo.Store, adapter Adapter, opts ...Option) Service {
	if adapter == nil {
		adapter = NoopAdapter{}
	}
	s := &impl{store: store, adapter: adapter}
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
	valid, problems, err := s.Validate(ctx, tenantID, actionTypeID, parameters)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"valid":          valid,
		"errors":         problems,
		"action_type_id": actionTypeID.String(),
		"parameters":     parameters,
		"adapter":        "noop",
	}, nil
}

func (s *impl) Run(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any) (domain.Execution, error) {
	valid, problems, err := s.Validate(ctx, tenantID, actionTypeID, parameters)
	if err != nil {
		return domain.Execution{}, err
	}
	if !valid {
		return domain.Execution{}, fmt.Errorf("%w: %s", domain.ErrInvalidInput, strings.Join(problems, "; "))
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	execution, err := s.store.CreateExecution(ctx, tenantID, actionTypeID, parameters, domain.ExecutionStatusQueued)
	if err != nil {
		return domain.Execution{}, err
	}
	if err := s.adapter.Start(ctx, actionTypeID, execution.ID); err != nil {
		if _, updateErr := s.store.UpdateExecutionStatus(ctx, tenantID, execution.ID, domain.ExecutionStatusFailed, err.Error()); updateErr != nil {
			return domain.Execution{}, fmt.Errorf("start action: %w; update execution: %v", err, updateErr)
		}
		return domain.Execution{}, fmt.Errorf("start action: %w", err)
	}
	return execution, nil
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
	if execution.Status == domain.ExecutionStatusSucceeded || execution.Status == domain.ExecutionStatusCancelled {
		return domain.Execution{}, domain.ErrInvalidInput
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

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
