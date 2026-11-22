package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

// fakeRuntimeStore implements the runtime slice of the persistence port on top
// of an in-memory execution row. Methods the service does not exercise during
// these tests are inherited from the nil embedded interfaces.
type fakeRuntimeStore struct {
	repo.Store
	repo.RuntimeStore

	actionType domain.ActionType
	execution  domain.Execution
	task       domain.ApprovalTask

	createCalls      int
	updateCalls      int
	decideCalls      int
	expireCalls      int
	lastPatch        repo.ExecutionPatch
	lastFromStatuses []string
	lastDecision     string
	idempotency      map[string]domain.IdempotencyRecord
}

func (f *fakeRuntimeStore) GetActionType(_ context.Context, _ uuid.UUID, _ uuid.UUID) (domain.ActionType, error) {
	return f.actionType, nil
}

func (f *fakeRuntimeStore) GetExecution(_ context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	if f.execution.ID == uuid.Nil || f.execution.ID != id || f.execution.TenantID != tenantID {
		return domain.Execution{}, domain.ErrNotFound
	}
	return f.execution, nil
}

func (f *fakeRuntimeStore) CreateExecution(_ context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any, status string) (domain.Execution, error) {
	f.execution = domain.Execution{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ActionTypeID: actionTypeID,
		Status:       status,
		Parameters:   parameters,
		CreatedAt:    time.Now().UTC(),
	}
	return f.execution, nil
}

func (f *fakeRuntimeStore) CreateExecutionWithDefinition(_ context.Context, tenantID, actionTypeID uuid.UUID, objectID *uuid.UUID, parameters, definition map[string]any) (domain.Execution, error) {
	f.createCalls++
	f.execution = domain.Execution{
		ID:                 uuid.New(),
		TenantID:           tenantID,
		ActionTypeID:       actionTypeID,
		ObjectID:           objectID,
		Status:             domain.ExecutionStatusQueued,
		Parameters:         parameters,
		DefinitionSnapshot: definition,
		CreatedAt:          time.Now().UTC(),
	}
	return f.execution, nil
}

func (f *fakeRuntimeStore) PutIdempotency(_ context.Context, tenantID uuid.UUID, key, requestHash string, executionID uuid.UUID, response map[string]any) (domain.IdempotencyRecord, error) {
	if f.idempotency == nil {
		f.idempotency = map[string]domain.IdempotencyRecord{}
	}
	if existing, ok := f.idempotency[key]; ok {
		if existing.RequestHash != requestHash {
			return domain.IdempotencyRecord{}, domain.ErrConflict
		}
		return existing, nil
	}
	record := domain.IdempotencyRecord{TenantID: tenantID, Key: key, RequestHash: requestHash, ExecutionID: executionID, Response: response, CreatedAt: time.Now().UTC()}
	f.idempotency[key] = record
	return record, nil
}

func (f *fakeRuntimeStore) GetIdempotency(_ context.Context, tenantID uuid.UUID, key string) (domain.IdempotencyRecord, error) {
	if f.idempotency == nil {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	record, ok := f.idempotency[key]
	if !ok || record.TenantID != tenantID {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	return record, nil
}

func (f *fakeRuntimeStore) UpdateExecutionRuntime(_ context.Context, tenantID, id uuid.UUID, patch repo.ExecutionPatch, fromStatuses []string) (domain.Execution, error) {
	f.updateCalls++
	f.lastPatch = patch
	f.lastFromStatuses = append([]string(nil), fromStatuses...)
	if f.execution.ID == uuid.Nil || f.execution.ID != id || f.execution.TenantID != tenantID {
		return domain.Execution{}, domain.ErrNotFound
	}
	matched := false
	for _, from := range fromStatuses {
		if from == f.execution.Status {
			matched = true
			break
		}
	}
	if !matched {
		return domain.Execution{}, domain.ErrConflict
	}
	applyFakeExecutionPatch(&f.execution, patch)
	return f.execution, nil
}

func (f *fakeRuntimeStore) GetApprovalTask(_ context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error) {
	if f.task.ID == uuid.Nil || f.task.ID != id || f.task.TenantID != tenantID {
		return domain.ApprovalTask{}, domain.ErrNotFound
	}
	return f.task, nil
}

func (f *fakeRuntimeStore) ListApprovalTasks(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]domain.ApprovalTask, int64, error) {
	if f.task.ID == uuid.Nil {
		return nil, 0, nil
	}
	return []domain.ApprovalTask{f.task}, 1, nil
}

func (f *fakeRuntimeStore) DecideApprovalTask(_ context.Context, tenantID, id uuid.UUID, decision, actorID, comment string) (domain.ApprovalTask, error) {
	f.decideCalls++
	f.lastDecision = decision
	if f.task.ID == uuid.Nil || f.task.ID != id || f.task.TenantID != tenantID {
		return domain.ApprovalTask{}, domain.ErrNotFound
	}
	if f.task.Status != domain.ApprovalTaskStatusPending {
		return domain.ApprovalTask{}, domain.ErrConflict
	}
	status := domain.ApprovalTaskStatusApproved
	if decision == "reject" {
		status = domain.ApprovalTaskStatusRejected
		f.task.ReasonCode = repo.ApprovalReasonRejected
	}
	now := time.Now().UTC()
	decidedBy, err := uuid.Parse(actorID)
	if err != nil {
		return domain.ApprovalTask{}, domain.ErrInvalidInput
	}
	f.task.Status = status
	f.task.DecidedAt = &now
	f.task.DecidedBy = &decidedBy
	f.task.Comment = comment
	return f.task, nil
}

func (f *fakeRuntimeStore) ExpireApprovalTasks(_ context.Context, _ time.Time) (int64, error) {
	f.expireCalls++
	if f.task.ID != uuid.Nil && f.task.Status == domain.ApprovalTaskStatusPending {
		f.task.Status = domain.ApprovalTaskStatusExpired
		return 1, nil
	}
	return 0, nil
}

func applyFakeExecutionPatch(execution *domain.Execution, patch repo.ExecutionPatch) {
	if patch.Status != nil {
		execution.Status = *patch.Status
	}
	if patch.WorkflowID != nil {
		execution.WorkflowID = *patch.WorkflowID
	}
	if patch.WorkflowRunID != nil {
		execution.WorkflowRunID = *patch.WorkflowRunID
	}
	if patch.FailureCode != nil {
		execution.FailureCode = *patch.FailureCode
	}
	if patch.FailureMessage != nil {
		execution.FailureMessage = *patch.FailureMessage
	}
	if patch.StartedAt != nil {
		execution.StartedAt = patch.StartedAt
	}
	if patch.FinishedAt != nil {
		execution.FinishedAt = patch.FinishedAt
	}
	if patch.Result != nil {
		execution.Result = patch.Result
	}
	if patch.Attempt != nil {
		execution.Attempt = *patch.Attempt
	}
	if patch.CurrentStepIndex != nil {
		execution.CurrentStepIndex = *patch.CurrentStepIndex
	}
	if patch.ApprovalState != nil {
		execution.ApprovalState = *patch.ApprovalState
	}
	if patch.CancelRequestedAt != nil {
		execution.CancelRequestedAt = patch.CancelRequestedAt
	}
	if patch.ObjectID != nil {
		execution.ObjectID = patch.ObjectID
	}
}

type fakeTemporalAdapter struct {
	mode ExecutionMode

	started    bool
	startCalls int
	startInput ExecutionInput
	startRef   ExecutionRef
	startErr   error

	cancelledRef ExecutionRef
	cancelReason string
	cancelErr    error
	signalErr    error
	signalledRef ExecutionRef
	decision     AdmissionDecision
	describeInfo ExecutionInfo
	healthErr    error
}

func (f *fakeTemporalAdapter) Mode() ExecutionMode {
	if f.mode == "" {
		return ExecutionModeTemporal
	}
	return f.mode
}

func (f *fakeTemporalAdapter) Start(_ context.Context, in ExecutionInput) (ExecutionRef, error) {
	f.started = true
	f.startCalls++
	f.startInput = in
	if f.startErr != nil {
		return ExecutionRef{}, f.startErr
	}
	ref := f.startRef
	if ref.ExecutionID == uuid.Nil {
		ref.ExecutionID = in.ExecutionID
	}
	return ref, nil
}

func (f *fakeTemporalAdapter) Cancel(_ context.Context, ref ExecutionRef, reason string) error {
	f.cancelledRef = ref
	f.cancelReason = reason
	return f.cancelErr
}

func (f *fakeTemporalAdapter) SignalApproval(_ context.Context, ref ExecutionRef, decision AdmissionDecision) error {
	f.signalledRef = ref
	f.decision = decision
	return f.signalErr
}

func (f *fakeTemporalAdapter) Describe(_ context.Context, _ ExecutionRef) (ExecutionInfo, error) {
	return f.describeInfo, nil
}

func (f *fakeTemporalAdapter) Health(context.Context) error {
	return f.healthErr
}

func temporalActionType() domain.ActionType {
	return domain.ActionType{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Name:     "update-object",
		Status:   domain.ActionStatusActive,
		Parameters: map[string]any{
			"required": []any{"reason"},
			"execution_definition": map[string]any{
				"steps": []any{
					map[string]any{
						"id":            "update",
						"activity_type": domain.ActivityTypeObjectUpdate,
						"parameters":    map[string]any{"field": "status"},
						"compensation": map[string]any{
							"activity_type": domain.ActivityTypeObjectUpdate,
							"parameters":    map[string]any{"field": "status"},
						},
					},
				},
			},
		},
	}
}

func TestRunStartsTemporalExecutionAndPersistsWorkflowReference(t *testing.T) {
	store := &fakeRuntimeStore{actionType: temporalActionType()}
	adapter := &fakeTemporalAdapter{startRef: ExecutionRef{WorkflowID: "action-execution-x", RunID: "run-1"}}
	svc := New(store, adapter)

	execution, err := svc.Run(context.Background(), store.actionType.TenantID, store.actionType.ID, map[string]any{"reason": "manual"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !adapter.started {
		t.Fatal("adapter Start was not called")
	}
	if adapter.startInput.ExecutionID != execution.ID {
		t.Fatalf("start input execution id = %s, want %s", adapter.startInput.ExecutionID, execution.ID)
	}
	if store.createCalls != 1 {
		t.Fatalf("CreateExecutionWithDefinition calls = %d, want 1", store.createCalls)
	}
	if len(store.execution.DefinitionSnapshot) == 0 || store.execution.DefinitionSnapshot["steps"] == nil {
		t.Fatalf("definition snapshot was not persisted: %#v", store.execution.DefinitionSnapshot)
	}
	if execution.WorkflowID != "action-execution-x" || execution.WorkflowRunID != "run-1" {
		t.Fatalf("workflow reference = (%q,%q), want (action-execution-x, run-1)", execution.WorkflowID, execution.WorkflowRunID)
	}
	if update := store.lastPatch; update.WorkflowID == nil || *update.WorkflowID != "action-execution-x" {
		t.Fatalf("workflow id was not persisted through UpdateExecutionRuntime: %#v", update)
	}
	if got, want := store.lastFromStatuses, []string{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning, domain.ExecutionStatusWaitingApproval}; len(got) != len(want) {
		t.Fatalf("from statuses = %v, want %v", got, want)
	}
}

func TestRunMarksExecutionFailedWhenEngineUnavailable(t *testing.T) {
	store := &fakeRuntimeStore{actionType: temporalActionType()}
	adapter := &fakeTemporalAdapter{startErr: errors.New("dial tcp 127.0.0.1:7233: connect: connection refused")}
	svc := New(store, adapter)

	_, err := svc.Run(context.Background(), store.actionType.TenantID, store.actionType.ID, map[string]any{"reason": "manual"})
	if !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("Run() error = %v, want ErrEngineUnavailable", err)
	}
	if store.execution.Status != domain.ExecutionStatusFailed {
		t.Fatalf("execution status = %q, want failed", store.execution.Status)
	}
	if store.execution.FailureCode != "engine_unavailable" {
		t.Fatalf("failure code = %q, want engine_unavailable", store.execution.FailureCode)
	}
	if update := store.lastPatch; update.FailureMessage == nil || *update.FailureMessage == "" {
		t.Fatal("engine failure message was not persisted")
	}
}

func TestPreviewReportsTemporalModeAndValidatedDefinition(t *testing.T) {
	store := &fakeRuntimeStore{actionType: temporalActionType()}
	svc := New(store, &fakeTemporalAdapter{})

	preview, err := svc.Preview(context.Background(), store.actionType.TenantID, store.actionType.ID, map[string]any{"reason": "manual"})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview["mode"] != string(ExecutionModeTemporal) {
		t.Fatalf("preview mode = %v, want %q", preview["mode"], ExecutionModeTemporal)
	}
	if preview["adapter"] != string(ExecutionModeTemporal) {
		t.Fatalf("preview adapter = %v, want %q", preview["adapter"], ExecutionModeTemporal)
	}
	if preview["valid"] != true {
		t.Fatalf("preview valid = %v, want true (errors: %v)", preview["valid"], preview["errors"])
	}
	if preview["execution_definition"] == nil {
		t.Fatal("preview is missing the validated execution definition")
	}
}

func TestPreviewRejectsInvalidExecutionDefinition(t *testing.T) {
	actionType := temporalActionType()
	actionType.Parameters["execution_definition"] = map[string]any{
		"steps": []any{
			map[string]any{"id": "broken", "activity_type": "not.supported"},
		},
	}
	store := &fakeRuntimeStore{actionType: actionType}
	svc := New(store, &fakeTemporalAdapter{})

	preview, err := svc.Preview(context.Background(), actionType.TenantID, actionType.ID, map[string]any{"reason": "manual"})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview["valid"] != false {
		t.Fatalf("preview valid = %v, want false", preview["valid"])
	}
	problems, ok := preview["errors"].([]string)
	if !ok || len(problems) == 0 {
		t.Fatalf("preview errors = %#v, want non-empty problems", preview["errors"])
	}
}

func TestCancelExecutionRequestsEngineCancellation(t *testing.T) {
	actionType := temporalActionType()
	executionID := uuid.New()
	store := &fakeRuntimeStore{
		actionType: actionType,
		execution: domain.Execution{
			ID:            executionID,
			TenantID:      actionType.TenantID,
			ActionTypeID:  actionType.ID,
			Status:        domain.ExecutionStatusRunning,
			WorkflowID:    "action-execution-x",
			WorkflowRunID: "run-1",
		},
	}
	adapter := &fakeTemporalAdapter{}
	svc := New(store, adapter)

	execution, err := svc.CancelExecution(context.Background(), actionType.TenantID, executionID)
	if err != nil {
		t.Fatalf("CancelExecution() error = %v", err)
	}
	if adapter.cancelledRef.WorkflowID != "action-execution-x" || adapter.cancelledRef.RunID != "run-1" {
		t.Fatalf("adapter cancel ref = %+v", adapter.cancelledRef)
	}
	if execution.Status != domain.ExecutionStatusCancelled {
		t.Fatalf("execution status = %q, want cancelled", execution.Status)
	}
	if store.execution.FailureCode != "cancelled" {
		t.Fatalf("failure code = %q, want cancelled", store.execution.FailureCode)
	}
}

func TestCancelExecutionRejectsTerminalExecution(t *testing.T) {
	actionType := temporalActionType()
	executionID := uuid.New()
	store := &fakeRuntimeStore{
		actionType: actionType,
		execution: domain.Execution{
			ID:           executionID,
			TenantID:     actionType.TenantID,
			ActionTypeID: actionType.ID,
			Status:       domain.ExecutionStatusSucceeded,
		},
	}
	svc := New(store, &fakeTemporalAdapter{})

	if _, err := svc.CancelExecution(context.Background(), actionType.TenantID, executionID); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("CancelExecution() error = %v, want ErrInvalidInput", err)
	}
	if store.updateCalls != 0 {
		t.Fatalf("terminal execution was modified: update calls = %d", store.updateCalls)
	}
}

func newPendingApprovalFixture(t *testing.T) (*fakeRuntimeStore, *fakeTemporalAdapter, domain.ApprovalTask, uuid.UUID) {
	t.Helper()
	actionType := temporalActionType()
	executionID := uuid.New()
	now := time.Now().UTC()
	task := domain.ApprovalTask{
		ID:          uuid.New(),
		TenantID:    actionType.TenantID,
		ExecutionID: executionID,
		StepIndex:   0,
		Status:      domain.ApprovalTaskStatusPending,
		RequestedAt: now,
		DeadlineAt:  now.Add(30 * time.Minute),
	}
	store := &fakeRuntimeStore{
		actionType: actionType,
		execution: domain.Execution{
			ID:            executionID,
			TenantID:      actionType.TenantID,
			ActionTypeID:  actionType.ID,
			Status:        domain.ExecutionStatusWaitingApproval,
			WorkflowID:    "action-execution-x",
			WorkflowRunID: "run-1",
		},
		task: task,
	}
	return store, &fakeTemporalAdapter{}, task, actionType.TenantID
}

func TestDecideApprovalTaskApprovesAndSignalsWorkflow(t *testing.T) {
	store, adapter, task, tenantID := newPendingApprovalFixture(t)
	svc := New(store, adapter)
	actor := uuid.New()

	decided, err := svc.DecideApprovalTask(context.Background(), tenantID, task.ID, true, actor, "looks good")
	if err != nil {
		t.Fatalf("DecideApprovalTask() error = %v", err)
	}
	if decided.Status != domain.ApprovalTaskStatusApproved {
		t.Fatalf("task status = %q, want approved", decided.Status)
	}
	if store.lastDecision != "approve" {
		t.Fatalf("decision = %q, want approve", store.lastDecision)
	}
	if !adapter.decision.Allowed {
		t.Fatalf("adapter decision = %+v, want allowed", adapter.decision)
	}
	if adapter.decision.ApproverID != actor || adapter.decision.Comment != "looks good" {
		t.Fatalf("adapter admission = %+v", adapter.decision)
	}
	if adapter.signalledRef.WorkflowID != "action-execution-x" || adapter.signalledRef.RunID != "run-1" {
		t.Fatalf("signal ref = %+v", adapter.signalledRef)
	}
}

func TestDecideApprovalTaskRejectionSignalsCompensationReason(t *testing.T) {
	store, adapter, task, tenantID := newPendingApprovalFixture(t)
	svc := New(store, adapter)

	decided, err := svc.DecideApprovalTask(context.Background(), tenantID, task.ID, false, uuid.New(), "not now")
	if err != nil {
		t.Fatalf("DecideApprovalTask() error = %v", err)
	}
	if decided.Status != domain.ApprovalTaskStatusRejected {
		t.Fatalf("task status = %q, want rejected", decided.Status)
	}
	if adapter.decision.Allowed {
		t.Fatalf("adapter decision = %+v, want not allowed", adapter.decision)
	}
	if adapter.decision.ReasonCode != repo.ApprovalReasonRejected {
		t.Fatalf("adapter reason code = %q, want %q", adapter.decision.ReasonCode, repo.ApprovalReasonRejected)
	}
}

func TestDecideApprovalTaskKeepsDecisionWhenSignalFails(t *testing.T) {
	store, adapter, task, tenantID := newPendingApprovalFixture(t)
	adapter.signalErr = errors.New("temporal unavailable")
	svc := New(store, adapter)

	decided, err := svc.DecideApprovalTask(context.Background(), tenantID, task.ID, true, uuid.New(), "looks good")
	if err == nil {
		t.Fatal("DecideApprovalTask() error = nil, want signal failure")
	}
	if decided.Status != domain.ApprovalTaskStatusApproved {
		t.Fatalf("decided task status = %q, want persisted approved decision", decided.Status)
	}
}

type legacyActionsStore struct {
	repo.Store
}

func TestApprovalEndpointsConflictWithoutRuntimeStore(t *testing.T) {
	svc := New(legacyActionsStore{}, NoopAdapter{})
	tenantID := uuid.New()

	if _, _, err := svc.ListApprovalTasks(context.Background(), tenantID, "", 50, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ListApprovalTasks() error = %v, want ErrConflict", err)
	}
	if _, err := svc.GetApprovalTask(context.Background(), tenantID, uuid.New()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("GetApprovalTask() error = %v, want ErrConflict", err)
	}
	if _, err := svc.DecideApprovalTask(context.Background(), tenantID, uuid.New(), true, uuid.New(), ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("DecideApprovalTask() error = %v, want ErrConflict", err)
	}
}

func TestRunIsIdempotentByContextKey(t *testing.T) {
	store := &fakeRuntimeStore{actionType: domain.ActionType{
		ID:         uuid.New(),
		Status:     domain.ActionStatusActive,
		Parameters: map[string]any{"execution_definition": map[string]any{"steps": []any{map[string]any{"id": "s1", "activity_type": "noop", "read_only": true}}}},
	}}
	adapter := &fakeTemporalAdapter{startRef: ExecutionRef{WorkflowID: "wf", RunID: "run"}}
	svc := New(store, adapter)
	tenant := uuid.New()
	ctx := WithIdempotencyKey(context.Background(), "run-key-1")

	first, err := svc.Run(ctx, tenant, store.actionType.ID, map[string]any{})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := svc.Run(ctx, tenant, store.actionType.ID, map[string]any{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected replay of %s, got %s", first.ID, second.ID)
	}
	if adapter.startCalls != 1 {
		t.Fatalf("expected one engine start, got %d", adapter.startCalls)
	}
	if _, err := svc.Run(ctx, tenant, store.actionType.ID, map[string]any{"different": true}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict for changed payload, got %v", err)
	}
}
