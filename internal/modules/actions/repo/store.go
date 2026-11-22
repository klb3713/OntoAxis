package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

type Store interface {
	CreateActionType(ctx context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error)
	GetActionType(ctx context.Context, tenantID, id uuid.UUID) (domain.ActionType, error)
	ListActionTypes(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.ActionType, int64, error)
	UpdateActionType(ctx context.Context, tenantID, id uuid.UUID, display string, parameters map[string]any) (domain.ActionType, error)
	DeleteActionType(ctx context.Context, tenantID, id uuid.UUID) error
	CreateExecution(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any, status string) (domain.Execution, error)
	GetExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error)
	ListExecutions(ctx context.Context, tenantID uuid.UUID, actionTypeID *uuid.UUID, limit, offset int) ([]domain.Execution, int64, error)
	UpdateExecutionStatus(ctx context.Context, tenantID, id uuid.UUID, status, message string) (domain.Execution, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// executionColumns is the canonical projection of action.action_executions.
// Keep it in sync with scanExecution.
const executionColumns = `id, tenant_id, action_type_id, status, parameters, result, error, created_at, started_at, finished_at,
object_id, definition_snapshot, workflow_id, workflow_run_id, attempt, current_step_index, approval_state,
failure_code, failure_message, cancel_requested_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// scanExecution scans executionColumns into a domain.Execution.
func scanExecution(row rowScanner) (domain.Execution, error) {
	var e domain.Execution
	var rawParameters, rawResult, rawDefinition []byte
	if err := row.Scan(
		&e.ID, &e.TenantID, &e.ActionTypeID, &e.Status, &rawParameters, &rawResult, &e.Error, &e.CreatedAt, &e.StartedAt, &e.FinishedAt,
		&e.ObjectID, &rawDefinition, &e.WorkflowID, &e.WorkflowRunID, &e.Attempt, &e.CurrentStepIndex, &e.ApprovalState,
		&e.FailureCode, &e.FailureMessage, &e.CancelRequestedAt,
	); err != nil {
		return domain.Execution{}, err
	}
	e.Parameters = decodeJSONMap(rawParameters)
	e.Result = decodeJSONMap(rawResult)
	e.DefinitionSnapshot = decodeJSONMap(rawDefinition)
	return e, nil
}

// decodeJSONMap decodes a nullable jsonb column into a map. Malformed or empty
// payloads yield nil instead of an error, matching the historic lenient scan
// behavior of this store.
func decodeJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// jsonPayload encodes a map for a jsonb parameter, or nil so the column keeps
// its SQL NULL when the input map is nil.
func jsonPayload(value map[string]any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func (s *PostgresStore) CreateActionType(ctx context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.ActionType{}, fmt.Errorf("generate action type id: %w", err)
	}
	payload, err := json.Marshal(parameters)
	if err != nil {
		return domain.ActionType{}, fmt.Errorf("encode parameters: %w", err)
	}
	const q = `INSERT INTO action.action_types (id, tenant_id, name, display_name, description, object_type_id, parameters, status)
VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
RETURNING id, tenant_id, name, display_name, description, object_type_id, parameters, status, created_at, updated_at`
	var a domain.ActionType
	var raw []byte
	err = s.pool.QueryRow(ctx, q, id, tenantID, name, display, description, objectTypeID, payload, status).Scan(
		&a.ID, &a.TenantID, &a.Name, &a.DisplayName, &a.Description, &a.ObjectTypeID, &raw, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return domain.ActionType{}, mapPgError(err)
	}
	_ = json.Unmarshal(raw, &a.Parameters)
	return a, nil
}

func (s *PostgresStore) GetActionType(ctx context.Context, tenantID, id uuid.UUID) (domain.ActionType, error) {
	const q = `SELECT id, tenant_id, name, display_name, description, object_type_id, parameters, status, created_at, updated_at
FROM action.action_types WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`
	var a domain.ActionType
	var raw []byte
	err := s.pool.QueryRow(ctx, q, tenantID, id).Scan(&a.ID, &a.TenantID, &a.Name, &a.DisplayName, &a.Description, &a.ObjectTypeID, &raw, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ActionType{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ActionType{}, err
	}
	_ = json.Unmarshal(raw, &a.Parameters)
	return a, nil
}

func (s *PostgresStore) ListActionTypes(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.ActionType, int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, name, display_name, description, object_type_id, parameters, status, created_at, updated_at
FROM action.action_types WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.ActionType{}
	for rows.Next() {
		var a domain.ActionType
		var raw []byte
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.DisplayName, &a.Description, &a.ObjectTypeID, &raw, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &a.Parameters)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM action.action_types WHERE tenant_id=$1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *PostgresStore) UpdateActionType(ctx context.Context, tenantID, id uuid.UUID, display string, parameters map[string]any) (domain.ActionType, error) {
	payload, err := json.Marshal(parameters)
	if err != nil {
		return domain.ActionType{}, fmt.Errorf("encode parameters: %w", err)
	}
	const q = `UPDATE action.action_types SET display_name=$3, parameters=$4::jsonb, updated_at=now()
WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
RETURNING id, tenant_id, name, display_name, description, object_type_id, parameters, status, created_at, updated_at`
	var a domain.ActionType
	var raw []byte
	err = s.pool.QueryRow(ctx, q, tenantID, id, display, payload).Scan(&a.ID, &a.TenantID, &a.Name, &a.DisplayName, &a.Description, &a.ObjectTypeID, &raw, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ActionType{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ActionType{}, err
	}
	_ = json.Unmarshal(raw, &a.Parameters)
	return a, nil
}

func (s *PostgresStore) DeleteActionType(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE action.action_types SET deleted_at=now(), updated_at=now() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateExecution(ctx context.Context, tenantID, actionTypeID uuid.UUID, parameters map[string]any, status string) (domain.Execution, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Execution{}, fmt.Errorf("generate execution id: %w", err)
	}
	payload, err := json.Marshal(parameters)
	if err != nil {
		return domain.Execution{}, fmt.Errorf("encode parameters: %w", err)
	}
	const q = `INSERT INTO action.action_executions (id, tenant_id, action_type_id, status, parameters)
VALUES ($1,$2,$3,$4,$5::jsonb)
RETURNING ` + executionColumns
	e, err := scanExecution(s.pool.QueryRow(ctx, q, id, tenantID, actionTypeID, status, payload))
	if err != nil {
		return domain.Execution{}, mapPgError(err)
	}
	return e, nil
}

func (s *PostgresStore) GetExecution(ctx context.Context, tenantID, id uuid.UUID) (domain.Execution, error) {
	const q = `SELECT ` + executionColumns + `
FROM action.action_executions WHERE tenant_id=$1 AND id=$2`
	e, err := scanExecution(s.pool.QueryRow(ctx, q, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Execution{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Execution{}, err
	}
	return e, nil
}

func (s *PostgresStore) ListExecutions(ctx context.Context, tenantID uuid.UUID, actionTypeID *uuid.UUID, limit, offset int) ([]domain.Execution, int64, error) {
	query := `SELECT ` + executionColumns + `
FROM action.action_executions WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	args := []any{tenantID, limit, offset}
	countQuery := `SELECT count(*) FROM action.action_executions WHERE tenant_id=$1`
	countArgs := []any{tenantID}
	if actionTypeID != nil {
		query = `SELECT ` + executionColumns + `
FROM action.action_executions WHERE tenant_id=$1 AND action_type_id=$2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`
		args = []any{tenantID, *actionTypeID, limit, offset}
		countQuery = `SELECT count(*) FROM action.action_executions WHERE tenant_id=$1 AND action_type_id=$2`
		countArgs = []any{tenantID, *actionTypeID}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Execution{}
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
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

// UpdateExecutionStatus is the legacy unconditional status update. New runtime
// transitions should use UpdateExecutionRuntime with a CAS fromStatuses guard.
func (s *PostgresStore) UpdateExecutionStatus(ctx context.Context, tenantID, id uuid.UUID, status, message string) (domain.Execution, error) {
	const q = `UPDATE action.action_executions
SET status=$3, error=$4, started_at=COALESCE(started_at, CASE WHEN $3='running' THEN now() END),
    finished_at=CASE WHEN $3 IN ('succeeded','failed','cancelled','timed_out','compensation_failed') THEN now() ELSE finished_at END
WHERE tenant_id=$1 AND id=$2
RETURNING ` + executionColumns
	e, err := scanExecution(s.pool.QueryRow(ctx, q, tenantID, id, status, message))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Execution{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Execution{}, err
	}
	return e, nil
}

func mapPgError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}
