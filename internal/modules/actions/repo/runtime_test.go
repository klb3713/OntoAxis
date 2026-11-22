//go:build integration

package repo_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
)

// The Postgres store must keep satisfying both the historic Store port and the
// new RuntimeStore port.
var (
	_ repo.Store        = (*repo.PostgresStore)(nil)
	_ repo.RuntimeStore = (*repo.PostgresStore)(nil)
)

// newRuntimeTestStore connects to DATABASE_URL and applies the actions
// migrations. Tests are skipped when DATABASE_URL is not set so the default
// `go test` invocation stays hermetic.
func newRuntimeTestStore(t *testing.T) (*repo.PostgresStore, *pgxpool.Pool) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		t.Skip("DATABASE_URL is not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping DATABASE_URL: %v", err)
	}
	applyActionMigrations(t, pool)
	return repo.New(pool), pool
}

func applyActionMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, path := range []string{
		"../migrations/20261122190000_actions_init.sql",
		"../migrations/20261122200000_action_temporal.sql",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		for _, statement := range migrationStatements(string(raw)) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("apply migration %s: %v", path, err)
			}
		}
	}
}

// migrationStatements extracts the Up section of a goose migration and splits
// it into executable statements using the same naive rules as the module
// migration runner (strip comment lines, split on semicolons).
func migrationStatements(source string) []string {
	if idx := strings.Index(source, "-- +goose Down"); idx >= 0 {
		source = source[:idx]
	}
	var kept strings.Builder
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept.WriteString(line)
		kept.WriteByte('\n')
	}
	out := make([]string, 0, 4)
	for _, part := range strings.Split(kept.String(), ";") {
		if statement := strings.TrimSpace(part); statement != "" {
			out = append(out, statement)
		}
	}
	return out
}

func newTestTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, statement := range []string{
			`DELETE FROM action.idempotency_keys WHERE tenant_id=$1`,
			`DELETE FROM action.approval_tasks WHERE tenant_id=$1`,
			`DELETE FROM action.execution_steps WHERE tenant_id=$1`,
			`DELETE FROM action.action_executions WHERE tenant_id=$1`,
			`DELETE FROM action.action_types WHERE tenant_id=$1`,
		} {
			if _, err := pool.Exec(ctx, statement, tenant); err != nil {
				t.Logf("clean up tenant %s: %v", tenant, err)
			}
		}
	})
	return tenant
}

func createTestActionType(t *testing.T, store *repo.PostgresStore, tenant uuid.UUID) domain.ActionType {
	t.Helper()
	actionType, err := store.CreateActionType(context.Background(), tenant, "runtime-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Runtime action", "", nil, nil, domain.ActionStatusActive)
	if err != nil {
		t.Fatalf("CreateActionType: %v", err)
	}
	return actionType
}

func createTestExecution(t *testing.T, store *repo.PostgresStore, tenant, actionTypeID uuid.UUID) domain.Execution {
	t.Helper()
	definition := map[string]any{
		"steps": []any{
			map[string]any{"id": "step-0", "activity_type": "noop", "read_only": true},
			map[string]any{"id": "step-1", "activity_type": "noop", "read_only": true},
		},
		"retry": map[string]any{"maximum_attempts": float64(3)},
	}
	execution, err := store.CreateExecutionWithDefinition(context.Background(), tenant, actionTypeID, nil, map[string]any{"object_id": "obj-1"}, definition)
	if err != nil {
		t.Fatalf("CreateExecutionWithDefinition: %v", err)
	}
	return execution
}

func ptr[T any](value T) *T { return &value }

func TestRuntimeExecutionLifecycle(t *testing.T) {
	store, pool := newRuntimeTestStore(t)
	ctx := context.Background()
	tenant := newTestTenant(t, pool)
	actionType := createTestActionType(t, store, tenant)
	execution := createTestExecution(t, store, tenant, actionType.ID)

	if execution.Status != domain.ExecutionStatusQueued {
		t.Fatalf("status = %q, want queued", execution.Status)
	}
	if execution.TenantID != tenant || execution.ActionTypeID != actionType.ID {
		t.Errorf("execution identity = %+v", execution)
	}
	if execution.Parameters["object_id"] != "obj-1" {
		t.Errorf("parameters = %+v", execution.Parameters)
	}
	if len(execution.DefinitionSnapshot) == 0 {
		t.Errorf("definition snapshot was not stored: %+v", execution.DefinitionSnapshot)
	}
	if execution.Attempt != 0 || execution.CurrentStepIndex != 0 || execution.ApprovalState != domain.ApprovalStateNone {
		t.Errorf("runtime defaults = %+v", execution)
	}
	if execution.StartedAt != nil || execution.FinishedAt != nil || execution.CancelRequestedAt != nil {
		t.Errorf("unexpected timestamps on fresh execution: %+v", execution)
	}

	if _, err := store.GetExecution(ctx, uuid.New(), execution.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-tenant GetExecution error = %v, want ErrNotFound", err)
	}

	startedAt := time.Now().UTC().Add(-time.Minute)
	running, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{
		Status:           ptr(domain.ExecutionStatusRunning),
		WorkflowID:       ptr("workflow-1"),
		WorkflowRunID:    ptr("run-1"),
		Attempt:          ptr(1),
		CurrentStepIndex: ptr(0),
		ApprovalState:    ptr(domain.ApprovalStatePending),
		StartedAt:        &startedAt,
	}, []string{domain.ExecutionStatusQueued})
	if err != nil {
		t.Fatalf("UpdateExecutionRuntime queued->running: %v", err)
	}
	if running.Status != domain.ExecutionStatusRunning || running.WorkflowID != "workflow-1" || running.WorkflowRunID != "run-1" {
		t.Errorf("running projection = %+v", running)
	}
	if running.Attempt != 1 || running.CurrentStepIndex != 0 || running.ApprovalState != domain.ApprovalStatePending {
		t.Errorf("running runtime fields = %+v", running)
	}
	if running.StartedAt == nil {
		t.Error("started_at was not set")
	}

	// The CAS must reject a stale source status without mutating the row.
	if _, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{
		FailureCode: ptr("stale"),
	}, []string{domain.ExecutionStatusQueued}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("stale CAS error = %v, want ErrConflict", err)
	}
	stored, err := store.GetExecution(ctx, tenant, execution.ID)
	if err != nil {
		t.Fatalf("GetExecution after stale CAS: %v", err)
	}
	if stored.Status != domain.ExecutionStatusRunning || stored.FailureCode != "" {
		t.Errorf("stale CAS mutated row: %+v", stored)
	}

	if _, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{
		Status: ptr(domain.ExecutionStatusRunning),
	}, nil); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty fromStatuses error = %v, want ErrInvalidInput", err)
	}
	if _, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{}, []string{domain.ExecutionStatusRunning}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty patch error = %v, want ErrInvalidInput", err)
	}
	if _, err := store.UpdateExecutionRuntime(ctx, uuid.New(), execution.ID, repo.ExecutionPatch{
		Status: ptr(domain.ExecutionStatusFailed),
	}, []string{domain.ExecutionStatusRunning}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-tenant CAS error = %v, want ErrNotFound", err)
	}
	if _, err := store.UpdateExecutionRuntime(ctx, tenant, uuid.New(), repo.ExecutionPatch{
		Status: ptr(domain.ExecutionStatusFailed),
	}, []string{domain.ExecutionStatusRunning}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing execution CAS error = %v, want ErrNotFound", err)
	}

	finishedAt := time.Now().UTC()
	succeeded, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{
		Status:     ptr(domain.ExecutionStatusSucceeded),
		FinishedAt: &finishedAt,
		Result:     map[string]any{"steps": float64(2), "ok": true},
	}, []string{domain.ExecutionStatusRunning})
	if err != nil {
		t.Fatalf("UpdateExecutionRuntime running->succeeded: %v", err)
	}
	if succeeded.Status != domain.ExecutionStatusSucceeded || succeeded.FinishedAt == nil {
		t.Errorf("succeeded projection = %+v", succeeded)
	}
	if succeeded.Result["ok"] != true || succeeded.Result["steps"] != float64(2) {
		t.Errorf("result = %+v", succeeded.Result)
	}

	// A terminal execution no longer matches a running source status, so a
	// stale engine update must lose the CAS; terminal immutability itself is
	// enforced by domain.CanTransition, not by the persistence CAS.
	if _, err := store.UpdateExecutionRuntime(ctx, tenant, execution.ID, repo.ExecutionPatch{
		Status: ptr(domain.ExecutionStatusFailed),
	}, []string{domain.ExecutionStatusRunning}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("terminal execution CAS error = %v, want ErrConflict", err)
	}
	afterCAS, err := store.GetExecution(ctx, tenant, execution.ID)
	if err != nil {
		t.Fatalf("GetExecution after terminal CAS: %v", err)
	}
	if afterCAS.Status != domain.ExecutionStatusSucceeded {
		t.Errorf("terminal status changed by losing CAS: %q", afterCAS.Status)
	}
}

func TestRuntimeLegacyStoreStillWorks(t *testing.T) {
	store, pool := newRuntimeTestStore(t)
	ctx := context.Background()
	tenant := newTestTenant(t, pool)
	actionType := createTestActionType(t, store, tenant)

	execution, err := store.CreateExecution(ctx, tenant, actionType.ID, map[string]any{"k": "v"}, domain.ExecutionStatusQueued)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if execution.Status != domain.ExecutionStatusQueued || execution.Parameters["k"] != "v" {
		t.Errorf("legacy CreateExecution projection = %+v", execution)
	}
	if execution.ApprovalState != domain.ApprovalStateNone || execution.Attempt != 0 {
		t.Errorf("legacy CreateExecution lost column defaults: %+v", execution)
	}

	running, err := store.UpdateExecutionStatus(ctx, tenant, execution.ID, domain.ExecutionStatusRunning, "")
	if err != nil {
		t.Fatalf("UpdateExecutionStatus running: %v", err)
	}
	if running.StartedAt == nil {
		t.Error("UpdateExecutionStatus did not set started_at for running")
	}
	succeeded, err := store.UpdateExecutionStatus(ctx, tenant, execution.ID, domain.ExecutionStatusSucceeded, "")
	if err != nil {
		t.Fatalf("UpdateExecutionStatus succeeded: %v", err)
	}
	if succeeded.FinishedAt == nil {
		t.Error("UpdateExecutionStatus did not set finished_at for terminal status")
	}
	if _, err := store.GetExecution(ctx, tenant, execution.ID); err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
}

func TestRuntimeStepLedger(t *testing.T) {
	store, pool := newRuntimeTestStore(t)
	ctx := context.Background()
	tenant := newTestTenant(t, pool)
	actionType := createTestActionType(t, store, tenant)
	execution := createTestExecution(t, store, tenant, actionType.ID)

	inserted, err := store.UpsertExecutionStep(ctx, tenant, domain.ExecutionStep{
		ExecutionID:    execution.ID,
		StepIndex:      0,
		ActivityType:   domain.ActivityTypeNoop,
		Status:         domain.ExecutionStepStatusPending,
		BeforeSnapshot: map[string]any{"before": "state"},
	})
	if err != nil {
		t.Fatalf("UpsertExecutionStep insert: %v", err)
	}
	if inserted.ID == uuid.Nil || inserted.TenantID != tenant || inserted.ExecutionID != execution.ID {
		t.Errorf("inserted step identity = %+v", inserted)
	}
	if inserted.Status != domain.ExecutionStepStatusPending || inserted.Attempt != 0 {
		t.Errorf("inserted step status = %+v", inserted)
	}
	if inserted.BeforeSnapshot["before"] != "state" || inserted.AfterSnapshot != nil {
		t.Errorf("inserted step snapshots = %+v", inserted)
	}
	if inserted.CreatedAt.IsZero() || inserted.UpdatedAt.IsZero() {
		t.Errorf("inserted step timestamps = %+v", inserted)
	}

	startedAt := time.Now().UTC().Add(-time.Minute)
	finishedAt := time.Now().UTC()
	updated, err := store.UpsertExecutionStep(ctx, tenant, domain.ExecutionStep{
		ID:             inserted.ID,
		ExecutionID:    execution.ID,
		StepIndex:      0,
		ActivityType:   domain.ActivityTypeObjectUpdate,
		Status:         domain.ExecutionStepStatusSucceeded,
		Attempt:        2,
		InputHash:      "hash-1",
		BeforeSnapshot: map[string]any{"before": "state"},
		AfterSnapshot:  map[string]any{"after": "state", "count": float64(2)},
		StartedAt:      &startedAt,
		FinishedAt:     &finishedAt,
	})
	if err != nil {
		t.Fatalf("UpsertExecutionStep update: %v", err)
	}
	if updated.ID != inserted.ID {
		t.Errorf("step row identity changed: %s -> %s", inserted.ID, updated.ID)
	}
	if updated.Status != domain.ExecutionStepStatusSucceeded || updated.Attempt != 2 || updated.InputHash != "hash-1" {
		t.Errorf("updated step = %+v", updated)
	}
	if updated.ActivityType != domain.ActivityTypeObjectUpdate {
		t.Errorf("updated step activity type = %q", updated.ActivityType)
	}
	if updated.AfterSnapshot["count"] != float64(2) {
		t.Errorf("updated step after snapshot = %+v", updated.AfterSnapshot)
	}
	if updated.StartedAt == nil || updated.FinishedAt == nil {
		t.Errorf("updated step timestamps = %+v", updated)
	}

	if _, err := store.UpsertExecutionStep(ctx, tenant, domain.ExecutionStep{
		ExecutionID:  execution.ID,
		StepIndex:    1,
		ActivityType: domain.ActivityTypeNoop,
	}); err != nil {
		t.Fatalf("UpsertExecutionStep second step: %v", err)
	}

	steps, err := store.ListExecutionSteps(ctx, tenant, execution.ID)
	if err != nil {
		t.Fatalf("ListExecutionSteps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 (upsert must not duplicate the slot)", len(steps))
	}
	if steps[0].StepIndex != 0 || steps[1].StepIndex != 1 {
		t.Errorf("steps order = %d,%d, want 0,1", steps[0].StepIndex, steps[1].StepIndex)
	}

	loaded, err := store.GetExecutionStep(ctx, tenant, execution.ID, 0)
	if err != nil {
		t.Fatalf("GetExecutionStep: %v", err)
	}
	if loaded.ID != inserted.ID || loaded.Status != domain.ExecutionStepStatusSucceeded {
		t.Errorf("loaded step = %+v", loaded)
	}
	if _, err := store.GetExecutionStep(ctx, tenant, execution.ID, 9); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing step error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetExecutionStep(ctx, uuid.New(), execution.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-tenant step error = %v, want ErrNotFound", err)
	}
	if other, err := store.ListExecutionSteps(ctx, uuid.New(), execution.ID); err != nil || len(other) != 0 {
		t.Errorf("cross-tenant ListExecutionSteps = %v, %v; want empty, nil", other, err)
	}

	if _, err := store.UpsertExecutionStep(ctx, uuid.New(), domain.ExecutionStep{
		ExecutionID:  execution.ID,
		StepIndex:    0,
		ActivityType: domain.ActivityTypeNoop,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("cross-tenant step upsert error = %v, want ErrConflict", err)
	}
}

func TestRuntimeApprovalTasks(t *testing.T) {
	store, pool := newRuntimeTestStore(t)
	ctx := context.Background()
	tenant := newTestTenant(t, pool)
	actionType := createTestActionType(t, store, tenant)
	execution := createTestExecution(t, store, tenant, actionType.ID)

	pending, err := store.CreateApprovalTask(ctx, tenant, domain.ApprovalTask{
		ExecutionID: execution.ID,
		StepIndex:   0,
	})
	if err != nil {
		t.Fatalf("CreateApprovalTask: %v", err)
	}
	if pending.ID == uuid.Nil || pending.TenantID != tenant || pending.Status != domain.ApprovalTaskStatusPending {
		t.Errorf("created approval task = %+v", pending)
	}
	if pending.RequestedAt.IsZero() {
		t.Error("requested_at default was not applied")
	}
	if got := pending.DeadlineAt.Sub(pending.RequestedAt); got != 30*time.Minute {
		t.Errorf("default deadline offset = %s, want 30m", got)
	}
	if _, err := store.CreateApprovalTask(ctx, tenant, domain.ApprovalTask{
		ExecutionID: execution.ID,
		StepIndex:   0,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate approval task error = %v, want ErrConflict", err)
	}

	pastRequested := time.Now().UTC().Add(-2 * time.Hour)
	futureDeadline := time.Now().UTC().Add(time.Hour)
	second, err := store.CreateApprovalTask(ctx, tenant, domain.ApprovalTask{
		ExecutionID: execution.ID,
		StepIndex:   1,
		RequestedAt: pastRequested,
		DeadlineAt:  futureDeadline,
	})
	if err != nil {
		t.Fatalf("CreateApprovalTask explicit times: %v", err)
	}

	listed, total, err := store.ListApprovalTasks(ctx, tenant, domain.ApprovalTaskStatusPending, 10, 0)
	if err != nil {
		t.Fatalf("ListApprovalTasks pending: %v", err)
	}
	if total != 2 || len(listed) != 2 {
		t.Errorf("pending tasks = %d/%d, want 2/2", len(listed), total)
	}
	if listed[0].ID != pending.ID || listed[1].ID != second.ID {
		t.Errorf("pending order = %s,%s; want newest requested first", listed[0].ID, listed[1].ID)
	}
	if limited, limitedTotal, err := store.ListApprovalTasks(ctx, tenant, domain.ApprovalTaskStatusPending, 1, 0); err != nil || len(limited) != 1 || limitedTotal != 2 {
		t.Errorf("limited list = %v/%d, %v; want 1/2, nil", len(limited), limitedTotal, err)
	}

	loaded, err := store.GetApprovalTask(ctx, tenant, pending.ID)
	if err != nil {
		t.Fatalf("GetApprovalTask: %v", err)
	}
	if loaded.ID != pending.ID {
		t.Errorf("loaded task = %+v", loaded)
	}
	if _, err := store.GetApprovalTask(ctx, uuid.New(), pending.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-tenant GetApprovalTask error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetApprovalTask(ctx, tenant, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing GetApprovalTask error = %v, want ErrNotFound", err)
	}

	if _, err := store.DecideApprovalTask(ctx, tenant, pending.ID, "maybe", "", ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid decision error = %v, want ErrInvalidInput", err)
	}
	if _, err := store.DecideApprovalTask(ctx, tenant, pending.ID, "approve", "not-a-uuid", ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid actor error = %v, want ErrInvalidInput", err)
	}

	approver := uuid.New()
	approved, err := store.DecideApprovalTask(ctx, tenant, pending.ID, "approve", approver.String(), "looks good")
	if err != nil {
		t.Fatalf("DecideApprovalTask approve: %v", err)
	}
	if approved.Status != domain.ApprovalTaskStatusApproved || approved.DecidedAt == nil {
		t.Errorf("approved task = %+v", approved)
	}
	if approved.DecidedBy == nil || *approved.DecidedBy != approver {
		t.Errorf("approved task decided_by = %v, want %s", approved.DecidedBy, approver)
	}
	if approved.Comment != "looks good" || approved.ReasonCode != "" {
		t.Errorf("approved task metadata = %+v", approved)
	}
	if _, err := store.DecideApprovalTask(ctx, tenant, pending.ID, "reject", "", ""); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("double decision error = %v, want ErrConflict", err)
	}
	if _, err := store.DecideApprovalTask(ctx, tenant, uuid.New(), "approve", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing decide error = %v, want ErrNotFound", err)
	}

	rejectApprover := uuid.New()
	rejected, err := store.DecideApprovalTask(ctx, tenant, second.ID, "rejected", rejectApprover.String(), "not allowed")
	if err != nil {
		t.Fatalf("DecideApprovalTask reject: %v", err)
	}
	if rejected.Status != domain.ApprovalTaskStatusRejected || rejected.ReasonCode != repo.ApprovalReasonRejected {
		t.Errorf("rejected task = %+v", rejected)
	}
	if rejected.Comment != "not allowed" || rejected.DecidedBy == nil || *rejected.DecidedBy != rejectApprover {
		t.Errorf("rejected task metadata = %+v", rejected)
	}

	expiring, err := store.CreateApprovalTask(ctx, tenant, domain.ApprovalTask{
		ExecutionID: execution.ID,
		StepIndex:   2,
		RequestedAt: time.Now().UTC().Add(-time.Hour),
		DeadlineAt:  time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateApprovalTask expiring: %v", err)
	}
	expired, err := store.ExpireApprovalTasks(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ExpireApprovalTasks: %v", err)
	}
	if expired < 1 {
		t.Errorf("ExpireApprovalTasks affected %d rows, want at least 1", expired)
	}
	expiredTask, err := store.GetApprovalTask(ctx, tenant, expiring.ID)
	if err != nil {
		t.Fatalf("GetApprovalTask expired: %v", err)
	}
	if expiredTask.Status != domain.ApprovalTaskStatusExpired || expiredTask.DecidedAt == nil || expiredTask.ReasonCode != repo.ApprovalReasonTimeout {
		t.Errorf("expired task = %+v", expiredTask)
	}

	all, allTotal, err := store.ListApprovalTasks(ctx, tenant, "all", 10, 0)
	if err != nil || allTotal != 3 || len(all) != 3 {
		t.Errorf("all tasks = %d/%d, %v; want 3/3, nil", len(all), allTotal, err)
	}
	approvedOnly, approvedTotal, err := store.ListApprovalTasks(ctx, tenant, domain.ApprovalTaskStatusApproved, 10, 0)
	if err != nil || approvedTotal != 1 || len(approvedOnly) != 1 || approvedOnly[0].ID != pending.ID {
		t.Errorf("approved tasks = %+v/%d, %v; want pending task only", approvedOnly, approvedTotal, err)
	}
	expiredOnly, expiredTotal, err := store.ListApprovalTasks(ctx, tenant, domain.ApprovalTaskStatusExpired, 10, 0)
	if err != nil || expiredTotal != 1 || len(expiredOnly) != 1 || expiredOnly[0].ID != expiring.ID {
		t.Errorf("expired tasks = %+v/%d, %v; want expiring task only", expiredOnly, expiredTotal, err)
	}
}

func TestRuntimeIdempotency(t *testing.T) {
	store, pool := newRuntimeTestStore(t)
	ctx := context.Background()
	tenant := newTestTenant(t, pool)
	otherTenant := newTestTenant(t, pool)
	actionType := createTestActionType(t, store, tenant)
	execution := createTestExecution(t, store, tenant, actionType.ID)

	record, err := store.PutIdempotency(ctx, tenant, "run-1", "hash-a", execution.ID, map[string]any{"execution_id": execution.ID.String()})
	if err != nil {
		t.Fatalf("PutIdempotency insert: %v", err)
	}
	if record.TenantID != tenant || record.Key != "run-1" || record.RequestHash != "hash-a" || record.ExecutionID != execution.ID {
		t.Errorf("idempotency record = %+v", record)
	}
	if record.Response["execution_id"] != execution.ID.String() {
		t.Errorf("idempotency response = %+v", record.Response)
	}
	if record.CreatedAt.IsZero() {
		t.Error("idempotency created_at was not set")
	}

	replay, err := store.PutIdempotency(ctx, tenant, "run-1", "hash-a", uuid.New(), nil)
	if err != nil {
		t.Fatalf("PutIdempotency replay: %v", err)
	}
	if replay.ExecutionID != execution.ID || replay.RequestHash != "hash-a" {
		t.Errorf("replayed record = %+v, want the first record preserved", replay)
	}
	if _, err := store.PutIdempotency(ctx, tenant, "run-1", "hash-b", uuid.New(), nil); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("changed request hash error = %v, want ErrConflict", err)
	}
	if _, err := store.PutIdempotency(ctx, tenant, "   ", "hash-a", execution.ID, nil); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty key error = %v, want ErrInvalidInput", err)
	}

	loaded, err := store.GetIdempotency(ctx, tenant, "run-1")
	if err != nil {
		t.Fatalf("GetIdempotency: %v", err)
	}
	if loaded.ExecutionID != execution.ID || loaded.RequestHash != "hash-a" {
		t.Errorf("loaded record = %+v", loaded)
	}
	if _, err := store.GetIdempotency(ctx, tenant, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing key error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetIdempotency(ctx, otherTenant, "run-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-tenant key error = %v, want ErrNotFound", err)
	}

	// The primary key is (tenant_id, key), so the same key is independent per
	// tenant and does not conflict across tenants.
	otherScope, err := store.PutIdempotency(ctx, otherTenant, "run-1", "hash-a", uuid.New(), nil)
	if err != nil {
		t.Fatalf("PutIdempotency other tenant: %v", err)
	}
	if otherScope.TenantID != otherTenant {
		t.Errorf("other tenant record = %+v", otherScope)
	}
}
