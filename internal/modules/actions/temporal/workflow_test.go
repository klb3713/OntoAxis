package temporal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/testsuite"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

// fakeExecutionStore is an in-memory, mutex-guarded ExecutionStore. It mirrors
// the runtime CAS semantics of repo.PostgresStore closely enough to exercise
// the workflow's idempotency and compensation paths without a database.
type fakeExecutionStore struct {
	mu         sync.Mutex
	executions map[uuid.UUID]domain.Execution
	steps      map[string]domain.ExecutionStep
	tasks      []domain.ApprovalTask
}

func newFakeExecutionStore(seed domain.Execution) *fakeExecutionStore {
	return &fakeExecutionStore{
		executions: map[uuid.UUID]domain.Execution{seed.ID: seed},
		steps:      map[string]domain.ExecutionStep{},
	}
}

func executionStepKey(executionID uuid.UUID, stepIndex int) string {
	return fmt.Sprintf("%s:%d", executionID, stepIndex)
}

func (s *fakeExecutionStore) GetExecution(_ context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	execution, ok := s.executions[id]
	if !ok || execution.TenantID != tenantID {
		return domain.Execution{}, fmt.Errorf("get execution: %w", domain.ErrNotFound)
	}
	return execution, nil
}

func (s *fakeExecutionStore) UpdateExecutionRuntime(_ context.Context, tenantID, id uuid.UUID, patch repo.ExecutionPatch, fromStatuses []string) (domain.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	execution, ok := s.executions[id]
	if !ok || execution.TenantID != tenantID {
		return domain.Execution{}, fmt.Errorf("update execution: %w", domain.ErrNotFound)
	}
	matched := false
	for _, status := range fromStatuses {
		if execution.Status == status {
			matched = true
			break
		}
	}
	if !matched {
		return domain.Execution{}, fmt.Errorf("update execution: %w: status %q not in %v", domain.ErrConflict, execution.Status, fromStatuses)
	}
	applyFakeExecutionPatch(&execution, patch)
	s.executions[id] = execution
	return execution, nil
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
	if patch.Attempt != nil {
		execution.Attempt = *patch.Attempt
	}
	if patch.CurrentStepIndex != nil {
		execution.CurrentStepIndex = *patch.CurrentStepIndex
	}
	if patch.ApprovalState != nil {
		execution.ApprovalState = *patch.ApprovalState
	}
	if patch.FailureCode != nil {
		execution.FailureCode = *patch.FailureCode
	}
	if patch.FailureMessage != nil {
		execution.FailureMessage = *patch.FailureMessage
	}
	if patch.CancelRequestedAt != nil {
		execution.CancelRequestedAt = patch.CancelRequestedAt
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
	if patch.ObjectID != nil {
		execution.ObjectID = patch.ObjectID
	}
}

func (s *fakeExecutionStore) GetExecutionStep(_ context.Context, tenantID, executionID uuid.UUID, stepIndex int) (domain.ExecutionStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	step, ok := s.steps[executionStepKey(executionID, stepIndex)]
	if !ok || step.TenantID != tenantID {
		return domain.ExecutionStep{}, fmt.Errorf("get execution step: %w", domain.ErrNotFound)
	}
	return step, nil
}

func (s *fakeExecutionStore) UpsertExecutionStep(_ context.Context, tenantID uuid.UUID, step domain.ExecutionStep) (domain.ExecutionStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := executionStepKey(step.ExecutionID, step.StepIndex)
	if existing, ok := s.steps[key]; ok {
		step.ID = existing.ID
	} else if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	step.TenantID = tenantID
	s.steps[key] = step
	return step, nil
}

func (s *fakeExecutionStore) CreateApprovalTask(_ context.Context, tenantID uuid.UUID, task domain.ApprovalTask) (domain.ApprovalTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tasks {
		if existing.TenantID == tenantID && existing.ExecutionID == task.ExecutionID &&
			existing.StepIndex == task.StepIndex && existing.Status == domain.ApprovalTaskStatusPending {
			return domain.ApprovalTask{}, fmt.Errorf("create approval task: %w", domain.ErrConflict)
		}
	}
	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}
	task.TenantID = tenantID
	s.tasks = append(s.tasks, task)
	return task, nil
}

// ExpireApprovalTasks expires every pending task. The workflow test clock is a
// mock clock, so comparing against the wall clock deadline would be flaky; the
// workflow test asserts the projection instead of the deadline comparison.
func (s *fakeExecutionStore) ExpireApprovalTasks(_ context.Context, _ time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired int64
	for index := range s.tasks {
		if s.tasks[index].Status == domain.ApprovalTaskStatusPending {
			s.tasks[index].Status = domain.ApprovalTaskStatusExpired
			expired++
		}
	}
	return expired, nil
}

func (s *fakeExecutionStore) snapshotExecution(id uuid.UUID) (domain.Execution, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	execution, ok := s.executions[id]
	return execution, ok
}

func (s *fakeExecutionStore) snapshotSteps() []domain.ExecutionStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	steps := make([]domain.ExecutionStep, 0, len(s.steps))
	for _, step := range s.steps {
		steps = append(steps, step)
	}
	return steps
}

func (s *fakeExecutionStore) snapshotTasks() []domain.ApprovalTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]domain.ApprovalTask, len(s.tasks))
	copy(tasks, s.tasks)
	return tasks
}

// fakeEffects records the order of applied and compensated side effects and can
// inject transient or permanent failures per activity type.
type fakeEffects struct {
	mu        sync.Mutex
	calls     []string
	transient map[string]int
	permanent map[string]int
}

func newFakeEffects() *fakeEffects {
	return &fakeEffects{
		transient: map[string]int{},
		permanent: map[string]int{},
	}
}

func (f *fakeEffects) Apply(_ context.Context, _, executionID uuid.UUID, step domain.StepDefinition, _ map[string]any) (map[string]any, map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, step.ActivityType)
	if remaining := f.permanent[step.ActivityType]; remaining > 0 {
		f.permanent[step.ActivityType] = remaining - 1
		return nil, nil, fmt.Errorf("%w: %s rejected", domain.ErrInvalidInput, step.ActivityType)
	}
	if remaining := f.transient[step.ActivityType]; remaining > 0 {
		f.transient[step.ActivityType] = remaining - 1
		return nil, nil, errors.New("transient upstream failure")
	}
	before := map[string]any{"step_id": step.ID, "execution_id": executionID.String()}
	after := map[string]any{"step_id": step.ID, "execution_id": executionID.String(), "applied": true}
	return before, after, nil
}

func (f *fakeEffects) snapshotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := make([]string, len(f.calls))
	copy(calls, f.calls)
	return calls
}

type workflowTestRig struct {
	store        *fakeExecutionStore
	effects      *fakeEffects
	env          *testsuite.TestWorkflowEnvironment
	tenantID     uuid.UUID
	executionID  uuid.UUID
	actionTypeID uuid.UUID
}

func newWorkflowTestRig(t *testing.T, seeded domain.Execution) *workflowTestRig {
	t.Helper()
	rig := &workflowTestRig{
		tenantID:     uuid.New(),
		executionID:  uuid.New(),
		actionTypeID: uuid.New(),
		effects:      newFakeEffects(),
	}
	seeded.ID = rig.executionID
	seeded.TenantID = rig.tenantID
	seeded.ActionTypeID = rig.actionTypeID
	if seeded.Status == "" {
		seeded.Status = domain.ExecutionStatusQueued
	}
	rig.store = newFakeExecutionStore(seeded)

	activities := &Activities{Store: rig.store, Effects: rig.effects}
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ActionExecutionWorkflow)
	env.RegisterActivity(activities)
	rig.env = env
	return rig
}

func (r *workflowTestRig) execute(t *testing.T, definition domain.ExecutionDefinition) (string, error) {
	t.Helper()
	r.env.ExecuteWorkflow(ActionExecutionWorkflow, WorkflowInput{
		TenantID:     r.tenantID,
		ExecutionID:  r.executionID,
		ActionTypeID: r.actionTypeID,
		Definition:   definition,
	})
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var status string
	err := r.env.GetWorkflowResult(&status)
	return status, err
}

func (r *workflowTestRig) execution(t *testing.T) domain.Execution {
	t.Helper()
	execution, ok := r.store.snapshotExecution(r.executionID)
	if !ok {
		t.Fatal("execution was not found in the store")
	}
	return execution
}

func (r *workflowTestRig) stepsByIndex(t *testing.T) map[int]domain.ExecutionStep {
	t.Helper()
	steps := map[int]domain.ExecutionStep{}
	for _, step := range r.store.snapshotSteps() {
		steps[step.StepIndex] = step
	}
	return steps
}

func TestActionExecutionWorkflowSucceeds(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{ID: "update", ActivityType: domain.ActivityTypeObjectUpdate},
			{ID: "create", ActivityType: domain.ActivityTypeObjectCreate, ReadOnly: true},
		},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusSucceeded {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusSucceeded)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusSucceeded {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusSucceeded)
	}
	if execution.FinishedAt == nil {
		t.Error("execution finished_at must be set")
	}
	steps := rig.stepsByIndex(t)
	if len(steps) != 2 {
		t.Fatalf("recorded %d steps, want 2", len(steps))
	}
	for index, step := range steps {
		if step.Status != domain.ExecutionStepStatusSucceeded {
			t.Errorf("step %d status = %q, want %q", index, step.Status, domain.ExecutionStepStatusSucceeded)
		}
	}
	calls := rig.effects.snapshotCalls()
	if len(calls) != 2 || calls[0] != domain.ActivityTypeObjectUpdate || calls[1] != domain.ActivityTypeObjectCreate {
		t.Fatalf("effect calls = %v, want [object.update object.create]", calls)
	}
}

func TestActionExecutionWorkflowRetriesTransientStepFailure(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	rig.effects.transient[domain.ActivityTypeObjectUpdate] = 1
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:           "update",
				ActivityType: domain.ActivityTypeObjectUpdate,
				Compensation: &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusSucceeded {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusSucceeded)
	}
	calls := rig.effects.snapshotCalls()
	if len(calls) != 2 {
		t.Fatalf("effect calls = %v, want two attempts", calls)
	}
	step, ok := rig.stepsByIndex(t)[0]
	if !ok {
		t.Fatal("step 0 ledger missing")
	}
	if step.Status != domain.ExecutionStepStatusSucceeded {
		t.Errorf("step 0 status = %q, want %q", step.Status, domain.ExecutionStepStatusSucceeded)
	}
	if step.Attempt != 2 {
		t.Errorf("step 0 attempt = %d, want 2", step.Attempt)
	}
}

func TestActionExecutionWorkflowCompensatesAfterRetryExhaustion(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	// The default retry policy allows three attempts; fail all of them.
	rig.effects.transient[domain.ActivityTypeObjectUpdate] = 3
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:           "update",
				ActivityType: domain.ActivityTypeObjectUpdate,
				Compensation: &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusFailed {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusFailed)
	}
	calls := rig.effects.snapshotCalls()
	if len(calls) != 4 {
		t.Fatalf("effect calls = %v, want three attempts plus one compensation", calls)
	}
	if calls[3] != domain.ActivityTypeObjectDelete {
		t.Fatalf("effect calls = %v, want the compensation last", calls)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusFailed {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusFailed)
	}
	step, ok := rig.stepsByIndex(t)[0]
	if !ok {
		t.Fatal("step 0 ledger missing")
	}
	if step.Status != domain.ExecutionStepStatusCompensated {
		t.Errorf("step 0 status = %q, want %q", step.Status, domain.ExecutionStepStatusCompensated)
	}
}

func TestActionExecutionWorkflowCompensatesInReverseAndReportsResiduals(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	rig.effects.permanent[domain.ActivityTypeTestPermanentFailure] = 1
	rig.effects.permanent[domain.ActivityTypeObjectDelete] = 1
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:           "one",
				ActivityType: domain.ActivityTypeObjectUpdate,
				Compensation: &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
			{
				ID:           "two",
				ActivityType: domain.ActivityTypeObjectCreate,
				Compensation: &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectUpdate},
			},
			{
				ID:                       "three",
				ActivityType:             domain.ActivityTypeTestPermanentFailure,
				AllowWithoutCompensation: true,
			},
		},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusCompensationFailed {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusCompensationFailed)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusCompensationFailed {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusCompensationFailed)
	}
	calls := rig.effects.snapshotCalls()
	want := []string{
		domain.ActivityTypeObjectUpdate,
		domain.ActivityTypeObjectCreate,
		domain.ActivityTypeTestPermanentFailure,
		domain.ActivityTypeObjectUpdate,
		domain.ActivityTypeObjectDelete,
	}
	if len(calls) != len(want) {
		t.Fatalf("effect calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("effect calls = %v, want %v", calls, want)
		}
	}
	steps := rig.stepsByIndex(t)
	if steps[0].Status != domain.ExecutionStepStatusCompensationFailed {
		t.Errorf("step 0 status = %q, want %q", steps[0].Status, domain.ExecutionStepStatusCompensationFailed)
	}
	if steps[1].Status != domain.ExecutionStepStatusCompensated {
		t.Errorf("step 1 status = %q, want %q", steps[1].Status, domain.ExecutionStepStatusCompensated)
	}
	if steps[2].Status != domain.ExecutionStepStatusFailed {
		t.Errorf("step 2 status = %q, want %q", steps[2].Status, domain.ExecutionStepStatusFailed)
	}
}

func TestActionExecutionWorkflowWaitsForApprovalAndContinues(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	approver := uuid.New()
	rig.env.RegisterDelayedCallback(func() {
		rig.env.SignalWorkflow(SignalApproval, service.AdmissionDecision{Allowed: true, ApproverID: approver})
	}, time.Second)
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:               "update",
				ActivityType:     domain.ActivityTypeObjectUpdate,
				RequiresApproval: true,
				Compensation:     &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		ApprovalTimeout: 30 * time.Minute,
		RetryPolicy:     domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusSucceeded {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusSucceeded)
	}
	execution := rig.execution(t)
	if execution.ApprovalState != domain.ApprovalStateApproved {
		t.Errorf("approval state = %q, want %q", execution.ApprovalState, domain.ApprovalStateApproved)
	}
	if tasks := rig.store.snapshotTasks(); len(tasks) != 1 {
		t.Fatalf("recorded %d approval tasks, want 1", len(tasks))
	}
	calls := rig.effects.snapshotCalls()
	if len(calls) != 1 || calls[0] != domain.ActivityTypeObjectUpdate {
		t.Fatalf("effect calls = %v, want [object.update]", calls)
	}
}

func TestActionExecutionWorkflowRejectsApproval(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	rig.env.RegisterDelayedCallback(func() {
		rig.env.SignalWorkflow(SignalApproval, service.AdmissionDecision{Allowed: false, ApproverID: uuid.New(), Comment: "not allowed"})
	}, time.Second)
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:               "update",
				ActivityType:     domain.ActivityTypeObjectUpdate,
				RequiresApproval: true,
				Compensation:     &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		ApprovalTimeout: 30 * time.Minute,
		RetryPolicy:     domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusCancelled {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusCancelled)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusCancelled {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusCancelled)
	}
	if execution.ApprovalState != domain.ApprovalStateRejected {
		t.Errorf("approval state = %q, want %q", execution.ApprovalState, domain.ApprovalStateRejected)
	}
	if execution.FailureCode != repo.ApprovalReasonRejected {
		t.Errorf("failure code = %q, want %q", execution.FailureCode, repo.ApprovalReasonRejected)
	}
	if calls := rig.effects.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("effect calls = %v, want none after rejection", calls)
	}
}

func TestActionExecutionWorkflowApprovalTimeout(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:               "update",
				ActivityType:     domain.ActivityTypeObjectUpdate,
				RequiresApproval: true,
				Compensation:     &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		ApprovalTimeout: time.Minute,
		RetryPolicy:     domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusTimedOut {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusTimedOut)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusTimedOut {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusTimedOut)
	}
	if execution.ApprovalState != domain.ApprovalStateExpired {
		t.Errorf("approval state = %q, want %q", execution.ApprovalState, domain.ApprovalStateExpired)
	}
	if execution.FailureCode != repo.ApprovalReasonTimeout {
		t.Errorf("failure code = %q, want %q", execution.FailureCode, repo.ApprovalReasonTimeout)
	}
	tasks := rig.store.snapshotTasks()
	if len(tasks) != 1 {
		t.Fatalf("recorded %d approval tasks, want 1", len(tasks))
	}
	if tasks[0].Status != domain.ApprovalTaskStatusExpired {
		t.Errorf("approval task status = %q, want %q", tasks[0].Status, domain.ApprovalTaskStatusExpired)
	}
	if calls := rig.effects.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("effect calls = %v, want none after approval timeout", calls)
	}
}

func TestActionExecutionWorkflowCompensatesOnCancellation(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{})
	rig.env.RegisterDelayedCallback(func() {
		rig.env.CancelWorkflow()
	}, time.Second)
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:           "update",
				ActivityType: domain.ActivityTypeObjectUpdate,
				Compensation: &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
			{
				ID:               "approval",
				ActivityType:     domain.ActivityTypeObjectCreate,
				RequiresApproval: true,
				Compensation:     &domain.CompensationSpec{ActivityType: domain.ActivityTypeObjectDelete},
			},
		},
		ApprovalTimeout: 30 * time.Minute,
		RetryPolicy:     domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	// A cancelled workflow may surface the Temporal cancellation error; the
	// durable projection is what matters for the product contract.
	if err != nil && status != domain.ExecutionStatusCancelled {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusCancelled {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusCancelled)
	}
	execution := rig.execution(t)
	if execution.Status != domain.ExecutionStatusCancelled {
		t.Errorf("execution status = %q, want %q", execution.Status, domain.ExecutionStatusCancelled)
	}
	steps := rig.stepsByIndex(t)
	if steps[0].Status != domain.ExecutionStepStatusCompensated {
		t.Errorf("step 0 status = %q, want %q", steps[0].Status, domain.ExecutionStepStatusCompensated)
	}
	calls := rig.effects.snapshotCalls()
	if len(calls) != 2 || calls[0] != domain.ActivityTypeObjectUpdate || calls[1] != domain.ActivityTypeObjectDelete {
		t.Fatalf("effect calls = %v, want [object.update object.delete]", calls)
	}
}

func TestActionExecutionWorkflowIgnoresTerminalExecution(t *testing.T) {
	rig := newWorkflowTestRig(t, domain.Execution{Status: domain.ExecutionStatusCancelled})
	definition := domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{ID: "update", ActivityType: domain.ActivityTypeObjectUpdate},
		},
		RetryPolicy: domain.DefaultRetryPolicy(),
	}

	status, err := rig.execute(t, definition)
	if err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
	if status != domain.ExecutionStatusCancelled {
		t.Fatalf("workflow status = %q, want %q", status, domain.ExecutionStatusCancelled)
	}
	if calls := rig.effects.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("effect calls = %v, want none for terminal execution", calls)
	}
	if steps := rig.store.snapshotSteps(); len(steps) != 0 {
		t.Fatalf("recorded %d steps for terminal execution, want 0", len(steps))
	}
}
