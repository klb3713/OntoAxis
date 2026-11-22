package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

type fakeStore struct {
	actionType domain.ActionType
	execution  domain.Execution
}

func (f *fakeStore) CreateActionType(_ context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error) {
	f.actionType = domain.ActionType{ID: uuid.New(), TenantID: tenantID, Name: name, DisplayName: display, Description: description, ObjectTypeID: objectTypeID, Parameters: parameters, Status: status}
	return f.actionType, nil
}
func (f *fakeStore) GetActionType(context.Context, uuid.UUID, uuid.UUID) (domain.ActionType, error) {
	return f.actionType, nil
}
func (f *fakeStore) ListActionTypes(context.Context, uuid.UUID, int, int) ([]domain.ActionType, int64, error) {
	return []domain.ActionType{f.actionType}, 1, nil
}
func (f *fakeStore) UpdateActionType(context.Context, uuid.UUID, uuid.UUID, string, map[string]any) (domain.ActionType, error) {
	return f.actionType, nil
}
func (f *fakeStore) DeleteActionType(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeStore) CreateExecution(_ context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any, status string) (domain.Execution, error) {
	f.execution = domain.Execution{ID: uuid.New(), TenantID: tenantID, ActionTypeID: actionTypeID, Parameters: parameters, Status: status}
	return f.execution, nil
}
func (f *fakeStore) GetExecution(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error) {
	return f.execution, nil
}
func (f *fakeStore) ListExecutions(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]domain.Execution, int64, error) {
	return []domain.Execution{f.execution}, 1, nil
}
func (f *fakeStore) UpdateExecutionStatus(_ context.Context, _ uuid.UUID, _ uuid.UUID, status, message string) (domain.Execution, error) {
	f.execution.Status = status
	f.execution.Error = message
	return f.execution, nil
}

func newTestService() (Service, *fakeStore) {
	store := &fakeStore{actionType: domain.ActionType{ID: uuid.New(), Name: "archive", DisplayName: "Archive", Status: domain.ActionStatusActive, Parameters: map[string]any{"required": []any{"reason"}}}}
	return New(store, NoopAdapter{}), store
}

func TestCreateActionTypeRejectsEmptyName(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.CreateActionType(context.Background(), uuid.New(), "", "", "", nil, nil, ""); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateActionTypeDefaultsStatus(t *testing.T) {
	svc, _ := newTestService()
	item, err := svc.CreateActionType(context.Background(), uuid.New(), "archive", "", "", nil, nil, "")
	if err != nil {
		t.Fatalf("create action type: %v", err)
	}
	if item.Status != domain.ActionStatusActive {
		t.Fatalf("status = %q, want active", item.Status)
	}
}

func TestValidateChecksRequiredParameters(t *testing.T) {
	svc, store := newTestService()
	valid, problems, err := svc.Validate(context.Background(), uuid.New(), store.actionType.ID, map[string]any{})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if valid || len(problems) != 1 {
		t.Fatalf("valid=%v problems=%v", valid, problems)
	}
}

func TestRunCreatesQueuedExecution(t *testing.T) {
	svc, store := newTestService()
	execution, err := svc.Run(context.Background(), uuid.New(), store.actionType.ID, map[string]any{"reason": "test"})
	if err != nil {
		t.Fatalf("run action: %v", err)
	}
	if execution.Status != domain.ExecutionStatusQueued {
		t.Fatalf("status = %q, want queued", execution.Status)
	}
}

func TestCancelRejectsSucceededExecution(t *testing.T) {
	svc, store := newTestService()
	store.execution = domain.Execution{ID: uuid.New(), TenantID: uuid.New(), Status: domain.ExecutionStatusSucceeded}
	if _, err := svc.CancelExecution(context.Background(), store.execution.TenantID, store.execution.ID); err == nil {
		t.Fatal("expected invalid input")
	}
}

type fakeTypeValidator struct {
	exists bool
}

func (f fakeTypeValidator) ObjectTypeExists(context.Context, string, string) (bool, error) {
	return f.exists, nil
}

func TestCreateActionTypeRejectsUnknownObjectType(t *testing.T) {
	store := &fakeStore{}
	svc := New(store, NoopAdapter{}, WithObjectTypeValidator(fakeTypeValidator{exists: false}))
	objectTypeID := uuid.New()
	if _, err := svc.CreateActionType(context.Background(), uuid.New(), "archive", "", "", &objectTypeID, nil, ""); err == nil {
		t.Fatal("expected unknown object type error")
	}
}
