package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

// Domain aliases keep the runtime persistence API readable while the canonical
// types stay in the domain package.
type (
	ExecutionStep     = domain.ExecutionStep
	ApprovalTask      = domain.ApprovalTask
	IdempotencyRecord = domain.IdempotencyRecord
)

// Reasons recorded on approval tasks and failure projections.
const (
	ApprovalReasonRejected = "approval_rejected"
	ApprovalReasonTimeout  = "approval_timeout"
)

// defaultApprovalTimeout is applied when a caller creates an approval task
// without an explicit deadline; the workflow normally passes the definition
// deadline instead.
const defaultApprovalTimeout = 30 * time.Minute

// ExecutionPatch is a partial update of the runtime projection of an execution.
// Nil fields are left untouched; Result and ObjectID are replaced when set.
type ExecutionPatch struct {
	Status            *string
	WorkflowID        *string
	WorkflowRunID     *string
	Attempt           *int
	CurrentStepIndex  *int
	ApprovalState     *string
	FailureCode       *string
	FailureMessage    *string
	CancelRequestedAt *time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	Result            map[string]any
	ObjectID          *uuid.UUID
}

// RuntimeStore is the persistence port used by the execution engine (Temporal
// workflow and activities). Implementations must scope every query by tenant
// and use conditional updates (CAS) for status changes.
type RuntimeStore interface {
	CreateExecutionWithDefinition(ctx context.Context, tenantID, actionTypeID uuid.UUID, objectID *uuid.UUID, parameters, definition map[string]any) (domain.Execution, error)
	UpdateExecutionRuntime(ctx context.Context, tenantID, id uuid.UUID, patch ExecutionPatch, fromStatuses []string) (domain.Execution, error)
	GetExecutionStep(ctx context.Context, tenantID, executionID uuid.UUID, stepIndex int) (domain.ExecutionStep, error)
	UpsertExecutionStep(ctx context.Context, tenantID uuid.UUID, step domain.ExecutionStep) (domain.ExecutionStep, error)
	ListExecutionSteps(ctx context.Context, tenantID, executionID uuid.UUID) ([]domain.ExecutionStep, error)
	CreateApprovalTask(ctx context.Context, tenantID uuid.UUID, task domain.ApprovalTask) (domain.ApprovalTask, error)
	GetApprovalTask(ctx context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error)
	ListApprovalTasks(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]domain.ApprovalTask, int64, error)
	DecideApprovalTask(ctx context.Context, tenantID, id uuid.UUID, decision, actorID, comment string) (domain.ApprovalTask, error)
	ExpireApprovalTasks(ctx context.Context, now time.Time) (int64, error)
	PutIdempotency(ctx context.Context, tenantID uuid.UUID, key, requestHash string, executionID uuid.UUID, response map[string]any) (domain.IdempotencyRecord, error)
	GetIdempotency(ctx context.Context, tenantID uuid.UUID, key string) (domain.IdempotencyRecord, error)
}

var _ RuntimeStore = (*PostgresStore)(nil)

const stepColumns = `id, tenant_id, execution_id, step_index, activity_type, status, attempt, input_hash,
before_snapshot, after_snapshot, error, started_at, finished_at, created_at, updated_at`

const approvalTaskColumns = `id, tenant_id, execution_id, step_index, status, requested_at, deadline_at,
decided_at, decided_by, comment, reason_code`

const idempotencyColumns = `tenant_id, key, request_hash, execution_id, response, created_at`

// CreateExecutionWithDefinition inserts a queued execution together with the
// execution definition snapshot captured at submission time.
func (s *PostgresStore) CreateExecutionWithDefinition(ctx context.Context, tenantID, actionTypeID uuid.UUID, objectID *uuid.UUID, parameters, definition map[string]any) (domain.Execution, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Execution{}, fmt.Errorf("generate execution id: %w", err)
	}
	parametersPayload, err := jsonPayload(parameters)
	if err != nil {
		return domain.Execution{}, fmt.Errorf("encode parameters: %w", err)
	}
	definitionPayload, err := jsonPayload(definition)
	if err != nil {
		return domain.Execution{}, fmt.Errorf("encode definition snapshot: %w", err)
	}
	if definitionPayload == nil {
		definitionPayload = []byte("{}")
	}
	const q = `INSERT INTO action.action_executions (id, tenant_id, action_type_id, object_id, status, parameters, definition_snapshot)
VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)
RETURNING ` + executionColumns
	e, err := scanExecution(s.pool.QueryRow(ctx, q, id, tenantID, actionTypeID, objectID, domain.ExecutionStatusQueued, parametersPayload, definitionPayload))
	if err != nil {
		return domain.Execution{}, mapPgError(err)
	}
	return e, nil
}

// UpdateExecutionRuntime applies a partial patch guarded by a status CAS. It
// returns domain.ErrConflict when the row exists but none of fromStatuses
// matched, and domain.ErrNotFound when the execution does not exist for the
// tenant. An empty fromStatuses list never matches, so callers must always
// state the expected source statuses.
func (s *PostgresStore) UpdateExecutionRuntime(ctx context.Context, tenantID, id uuid.UUID, patch ExecutionPatch, fromStatuses []string) (domain.Execution, error) {
	args := []any{id, tenantID}
	sets := make([]string, 0, 12)
	add := func(expression string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(expression, len(args)))
	}
	if patch.Status != nil {
		add("status=$%d", *patch.Status)
	}
	if patch.WorkflowID != nil {
		add("workflow_id=$%d", *patch.WorkflowID)
	}
	if patch.WorkflowRunID != nil {
		add("workflow_run_id=$%d", *patch.WorkflowRunID)
	}
	if patch.Attempt != nil {
		add("attempt=$%d", *patch.Attempt)
	}
	if patch.CurrentStepIndex != nil {
		add("current_step_index=$%d", *patch.CurrentStepIndex)
	}
	if patch.ApprovalState != nil {
		add("approval_state=$%d", *patch.ApprovalState)
	}
	if patch.FailureCode != nil {
		add("failure_code=$%d", *patch.FailureCode)
	}
	if patch.FailureMessage != nil {
		add("failure_message=$%d", *patch.FailureMessage)
	}
	if patch.CancelRequestedAt != nil {
		add("cancel_requested_at=$%d", *patch.CancelRequestedAt)
	}
	if patch.StartedAt != nil {
		add("started_at=$%d", *patch.StartedAt)
	}
	if patch.FinishedAt != nil {
		add("finished_at=$%d", *patch.FinishedAt)
	}
	if patch.ObjectID != nil {
		add("object_id=$%d", *patch.ObjectID)
	}
	if patch.Result != nil {
		payload, err := jsonPayload(patch.Result)
		if err != nil {
			return domain.Execution{}, fmt.Errorf("encode result: %w", err)
		}
		add("result=$%d::jsonb", payload)
	}
	if len(fromStatuses) == 0 {
		return domain.Execution{}, fmt.Errorf("%w: fromStatuses must not be empty", domain.ErrInvalidInput)
	}
	if len(sets) == 0 {
		return domain.Execution{}, fmt.Errorf("%w: patch must set at least one field", domain.ErrInvalidInput)
	}
	args = append(args, fromStatuses)
	query := fmt.Sprintf(`UPDATE action.action_executions SET %s
WHERE id=$1 AND tenant_id=$2 AND status = ANY($%d)
RETURNING %s`, strings.Join(sets, ", "), len(args), executionColumns)
	e, err := scanExecution(s.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := s.executionExists(ctx, tenantID, id)
		if existsErr != nil {
			return domain.Execution{}, existsErr
		}
		if exists {
			return domain.Execution{}, domain.ErrConflict
		}
		return domain.Execution{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Execution{}, mapPgError(err)
	}
	return e, nil
}

// executionExists reports whether the execution exists for the tenant. It lets
// a failed CAS distinguish a missing execution from a conflicting status.
func (s *PostgresStore) executionExists(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM action.action_executions WHERE id=$1 AND tenant_id=$2)`, id, tenantID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func scanExecutionStep(row rowScanner) (domain.ExecutionStep, error) {
	var step domain.ExecutionStep
	var rawBefore, rawAfter []byte
	if err := row.Scan(
		&step.ID, &step.TenantID, &step.ExecutionID, &step.StepIndex, &step.ActivityType, &step.Status, &step.Attempt, &step.InputHash,
		&rawBefore, &rawAfter, &step.Error, &step.StartedAt, &step.FinishedAt, &step.CreatedAt, &step.UpdatedAt,
	); err != nil {
		return domain.ExecutionStep{}, err
	}
	step.BeforeSnapshot = decodeJSONMap(rawBefore)
	step.AfterSnapshot = decodeJSONMap(rawAfter)
	return step, nil
}

// GetExecutionStep loads one step of an execution by its zero-based index.
func (s *PostgresStore) GetExecutionStep(ctx context.Context, tenantID, executionID uuid.UUID, stepIndex int) (domain.ExecutionStep, error) {
	const q = `SELECT ` + stepColumns + `
FROM action.execution_steps WHERE tenant_id=$1 AND execution_id=$2 AND step_index=$3`
	step, err := scanExecutionStep(s.pool.QueryRow(ctx, q, tenantID, executionID, stepIndex))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExecutionStep{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ExecutionStep{}, err
	}
	return step, nil
}

// UpsertExecutionStep inserts or updates the ledger row for (execution,
// step_index). The row identity is the unique step slot, so repeated activity
// attempts converge on a single row for at-least-once delivery.
func (s *PostgresStore) UpsertExecutionStep(ctx context.Context, tenantID uuid.UUID, step domain.ExecutionStep) (domain.ExecutionStep, error) {
	if step.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return domain.ExecutionStep{}, fmt.Errorf("generate execution step id: %w", err)
		}
		step.ID = id
	}
	if step.TenantID == uuid.Nil {
		step.TenantID = tenantID
	}
	if step.Status == "" {
		step.Status = domain.ExecutionStepStatusPending
	}
	beforePayload, err := jsonPayload(step.BeforeSnapshot)
	if err != nil {
		return domain.ExecutionStep{}, fmt.Errorf("encode before snapshot: %w", err)
	}
	afterPayload, err := jsonPayload(step.AfterSnapshot)
	if err != nil {
		return domain.ExecutionStep{}, fmt.Errorf("encode after snapshot: %w", err)
	}
	const q = `INSERT INTO action.execution_steps (id, tenant_id, execution_id, step_index, activity_type, status, attempt, input_hash, before_snapshot, after_snapshot, error, started_at, finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13)
ON CONFLICT (execution_id, step_index) DO UPDATE SET
    activity_type=EXCLUDED.activity_type,
    status=EXCLUDED.status,
    attempt=EXCLUDED.attempt,
    input_hash=EXCLUDED.input_hash,
    before_snapshot=EXCLUDED.before_snapshot,
    after_snapshot=EXCLUDED.after_snapshot,
    error=EXCLUDED.error,
    started_at=EXCLUDED.started_at,
    finished_at=EXCLUDED.finished_at,
    updated_at=now()
WHERE action.execution_steps.tenant_id = EXCLUDED.tenant_id
RETURNING ` + stepColumns
	stored, err := scanExecutionStep(s.pool.QueryRow(ctx, q,
		step.ID, tenantID, step.ExecutionID, step.StepIndex, step.ActivityType, step.Status, step.Attempt, step.InputHash,
		beforePayload, afterPayload, step.Error, step.StartedAt, step.FinishedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// The slot is owned by another tenant; do not leak its existence.
		return domain.ExecutionStep{}, domain.ErrConflict
	}
	if err != nil {
		return domain.ExecutionStep{}, mapPgError(err)
	}
	return stored, nil
}

// ListExecutionSteps returns the step ledger in step order.
func (s *PostgresStore) ListExecutionSteps(ctx context.Context, tenantID, executionID uuid.UUID) ([]domain.ExecutionStep, error) {
	const q = `SELECT ` + stepColumns + `
FROM action.execution_steps WHERE tenant_id=$1 AND execution_id=$2 ORDER BY step_index ASC`
	rows, err := s.pool.Query(ctx, q, tenantID, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ExecutionStep{}
	for rows.Next() {
		step, err := scanExecutionStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanApprovalTask(row rowScanner) (domain.ApprovalTask, error) {
	var task domain.ApprovalTask
	if err := row.Scan(
		&task.ID, &task.TenantID, &task.ExecutionID, &task.StepIndex, &task.Status, &task.RequestedAt, &task.DeadlineAt,
		&task.DecidedAt, &task.DecidedBy, &task.Comment, &task.ReasonCode,
	); err != nil {
		return domain.ApprovalTask{}, err
	}
	return task, nil
}

// CreateApprovalTask inserts one pending task per (execution, step) slot.
func (s *PostgresStore) CreateApprovalTask(ctx context.Context, tenantID uuid.UUID, task domain.ApprovalTask) (domain.ApprovalTask, error) {
	if task.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return domain.ApprovalTask{}, fmt.Errorf("generate approval task id: %w", err)
		}
		task.ID = id
	}
	if task.Status == "" {
		task.Status = domain.ApprovalTaskStatusPending
	}
	now := time.Now().UTC()
	if task.RequestedAt.IsZero() {
		task.RequestedAt = now
	}
	if task.DeadlineAt.IsZero() {
		task.DeadlineAt = task.RequestedAt.Add(defaultApprovalTimeout)
	}
	const q = `INSERT INTO action.approval_tasks (id, tenant_id, execution_id, step_index, status, requested_at, deadline_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING ` + approvalTaskColumns
	stored, err := scanApprovalTask(s.pool.QueryRow(ctx, q,
		task.ID, tenantID, task.ExecutionID, task.StepIndex, task.Status, task.RequestedAt, task.DeadlineAt,
	))
	if err != nil {
		return domain.ApprovalTask{}, mapPgError(err)
	}
	return stored, nil
}

// GetApprovalTask loads one approval task for the tenant.
func (s *PostgresStore) GetApprovalTask(ctx context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error) {
	const q = `SELECT ` + approvalTaskColumns + `
FROM action.approval_tasks WHERE tenant_id=$1 AND id=$2`
	task, err := scanApprovalTask(s.pool.QueryRow(ctx, q, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ApprovalTask{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ApprovalTask{}, err
	}
	return task, nil
}

// ListApprovalTasks lists approval tasks for the tenant, optionally filtered by
// status ("all" or "" lists every status). The total ignores limit/offset.
func (s *PostgresStore) ListApprovalTasks(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]domain.ApprovalTask, int64, error) {
	status = strings.TrimSpace(status)
	var (
		query      string
		args       []any
		countQuery string
		countArgs  []any
	)
	if status == "" || status == "all" {
		query = `SELECT ` + approvalTaskColumns + `
FROM action.approval_tasks WHERE tenant_id=$1 ORDER BY requested_at DESC, id DESC LIMIT $2 OFFSET $3`
		args = []any{tenantID, limit, offset}
		countQuery = `SELECT count(*) FROM action.approval_tasks WHERE tenant_id=$1`
		countArgs = []any{tenantID}
	} else {
		query = `SELECT ` + approvalTaskColumns + `
FROM action.approval_tasks WHERE tenant_id=$1 AND status=$2 ORDER BY requested_at DESC, id DESC LIMIT $3 OFFSET $4`
		args = []any{tenantID, status, limit, offset}
		countQuery = `SELECT count(*) FROM action.approval_tasks WHERE tenant_id=$1 AND status=$2`
		countArgs = []any{tenantID, status}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.ApprovalTask{}
	for rows.Next() {
		task, err := scanApprovalTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// DecideApprovalTask applies a decision with a pending-only CAS. Decisions are
// idempotent in effect: only the first call for a pending task succeeds, later
// calls return domain.ErrConflict without changing the stored task.
func (s *PostgresStore) DecideApprovalTask(ctx context.Context, tenantID, id uuid.UUID, decision, actorID, comment string) (domain.ApprovalTask, error) {
	status := ""
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", domain.ApprovalTaskStatusApproved:
		status = domain.ApprovalTaskStatusApproved
	case "reject", domain.ApprovalTaskStatusRejected:
		status = domain.ApprovalTaskStatusRejected
	default:
		return domain.ApprovalTask{}, fmt.Errorf("%w: decision must be %q or %q", domain.ErrInvalidInput, "approve", "reject")
	}
	var decidedBy *uuid.UUID
	if trimmed := strings.TrimSpace(actorID); trimmed != "" {
		actor, err := uuid.Parse(trimmed)
		if err != nil {
			return domain.ApprovalTask{}, fmt.Errorf("%w: actor_id must be a uuid", domain.ErrInvalidInput)
		}
		decidedBy = &actor
	}
	// A rejection records the stable reason code while approvals keep the
	// column untouched.
	q := fmt.Sprintf(`UPDATE action.approval_tasks
SET status=$3, decided_at=now(), decided_by=$4, comment=$5,
    reason_code=CASE WHEN $3='rejected' THEN '%s' ELSE reason_code END
WHERE tenant_id=$1 AND id=$2 AND status='pending'
RETURNING %s`, ApprovalReasonRejected, approvalTaskColumns)
	task, err := scanApprovalTask(s.pool.QueryRow(ctx, q, tenantID, id, status, decidedBy, comment))
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := s.approvalTaskExists(ctx, tenantID, id)
		if existsErr != nil {
			return domain.ApprovalTask{}, existsErr
		}
		if exists {
			return domain.ApprovalTask{}, domain.ErrConflict
		}
		return domain.ApprovalTask{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ApprovalTask{}, mapPgError(err)
	}
	return task, nil
}

func (s *PostgresStore) approvalTaskExists(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM action.approval_tasks WHERE tenant_id=$1 AND id=$2)`, tenantID, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ExpireApprovalTasks marks pending tasks whose deadline passed as expired and
// returns the number of affected rows. It is safe to run concurrently.
func (s *PostgresStore) ExpireApprovalTasks(ctx context.Context, now time.Time) (int64, error) {
	q := fmt.Sprintf(`UPDATE action.approval_tasks
SET status='expired', decided_at=$1,
    reason_code=CASE WHEN reason_code='' THEN '%s' ELSE reason_code END
WHERE status='pending' AND deadline_at <= $1`, ApprovalReasonTimeout)
	tag, err := s.pool.Exec(ctx, q, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanIdempotencyRecord(row rowScanner) (domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	var rawResponse []byte
	if err := row.Scan(&record.TenantID, &record.Key, &record.RequestHash, &record.ExecutionID, &rawResponse, &record.CreatedAt); err != nil {
		return domain.IdempotencyRecord{}, err
	}
	record.Response = decodeJSONMap(rawResponse)
	return record, nil
}

// PutIdempotency inserts an idempotency record. If the (tenant, key) pair
// already exists it returns the stored record when the request hash matches and
// domain.ErrConflict when it differs. Either way the first record is preserved.
func (s *PostgresStore) PutIdempotency(ctx context.Context, tenantID uuid.UUID, key, requestHash string, executionID uuid.UUID, response map[string]any) (domain.IdempotencyRecord, error) {
	if strings.TrimSpace(key) == "" {
		return domain.IdempotencyRecord{}, fmt.Errorf("%w: idempotency key is required", domain.ErrInvalidInput)
	}
	payload, err := jsonPayload(response)
	if err != nil {
		return domain.IdempotencyRecord{}, fmt.Errorf("encode idempotency response: %w", err)
	}
	const q = `INSERT INTO action.idempotency_keys (tenant_id, key, request_hash, execution_id, response)
VALUES ($1,$2,$3,$4,$5::jsonb)
ON CONFLICT (tenant_id, key) DO NOTHING
RETURNING ` + idempotencyColumns
	record, err := scanIdempotencyRecord(s.pool.QueryRow(ctx, q, tenantID, key, requestHash, executionID, payload))
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, mapPgError(err)
	}
	existing, getErr := s.GetIdempotency(ctx, tenantID, key)
	if getErr != nil {
		if errors.Is(getErr, domain.ErrNotFound) {
			// The conflicting record disappeared between insert and read;
			// report a conflict instead of silently retrying with stale state.
			return domain.IdempotencyRecord{}, domain.ErrConflict
		}
		return domain.IdempotencyRecord{}, getErr
	}
	if existing.RequestHash != requestHash {
		return domain.IdempotencyRecord{}, domain.ErrConflict
	}
	return existing, nil
}

// GetIdempotency loads the stored idempotency record for (tenant, key).
func (s *PostgresStore) GetIdempotency(ctx context.Context, tenantID uuid.UUID, key string) (domain.IdempotencyRecord, error) {
	const q = `SELECT ` + idempotencyColumns + `
FROM action.idempotency_keys WHERE tenant_id=$1 AND key=$2`
	record, err := scanIdempotencyRecord(s.pool.QueryRow(ctx, q, tenantID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.IdempotencyRecord{}, err
	}
	return record, nil
}
