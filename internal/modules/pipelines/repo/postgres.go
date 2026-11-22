package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

// PostgresStore is the production Repository. Every statement is explicitly
// tenant-scoped (in addition to RLS) and uses bound parameters only.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a store backed by the shared connection pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Ping checks database connectivity for module readiness.
func (s *PostgresStore) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("%w: database pool is not configured", domain.ErrUnavailable)
	}
	return s.pool.Ping(ctx)
}

const pipelineColumns = `id::text, tenant_id::text, name, display_name, description, pipeline_type, dag, version, status, created_by, deleted_at, created_at, updated_at`

// runColumns keeps every run SELECT/RETURNING in the same column order as
// scanRun. Migration 00002 made execution_id and failure_code nullable, so both
// are normalised to empty strings here instead of scanning NULL into a string.
const runColumns = `id::text, tenant_id::text, pipeline_id::text, version, status, trigger_type,
	COALESCE(execution_id, ''), COALESCE(failure_code, ''), attempt_count, last_reconciled_at,
	started_at, finished_at, error, created_at, updated_at`

const createRunSQL = `
		INSERT INTO pipeline.runs
			(id, tenant_id, pipeline_id, version, status, trigger_type, execution_id, failure_code,
			 attempt_count, last_reconciled_at, started_at, finished_at, error, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''),
			$9, $10, $11, $12, $13, $14, $15)`

// updateRunConditionalSQL is the only run write that can reach a terminal state.
// The status guard makes the write a compare-and-set: once a run is terminal the
// UPDATE affects no row, so a late or replayed reconciliation can never regress
// it (D7). Nullable references merge with COALESCE and the attempt counter only
// grows.
const updateRunConditionalSQL = `
		UPDATE pipeline.runs
		SET status = $3,
		    execution_id = COALESCE(NULLIF($4, ''), execution_id),
		    failure_code = COALESCE(NULLIF($5, ''), failure_code),
		    attempt_count = GREATEST(attempt_count, $6),
		    last_reconciled_at = COALESCE($7, last_reconciled_at),
		    started_at = COALESCE(started_at, $8),
		    finished_at = COALESCE(finished_at, $9),
		    error = COALESCE(NULLIF($10, ''), error),
		    updated_at = $11
		WHERE tenant_id = $1::uuid AND id = $2::uuid
		  AND status IN ('queued', 'running')
		RETURNING ` + runColumns

const insertRunInputSQL = `
				INSERT INTO pipeline.run_inputs (run_id, tenant_id, dataset_id, created_at)
				VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
				ON CONFLICT (run_id, dataset_id) DO NOTHING`

const insertRunOutputSQL = `
				INSERT INTO pipeline.run_outputs
					(run_id, tenant_id, dataset_id, model, relation_catalog, relation_schema, relation_table, created_at)
				VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8)
				ON CONFLICT (run_id, dataset_id) DO NOTHING`

// CreatePipeline inserts a new definition.
func (s *PostgresStore) CreatePipeline(ctx context.Context, pipeline domain.Pipeline) (domain.Pipeline, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pipeline.pipelines
			(id, tenant_id, name, display_name, description, pipeline_type, dag, version, status, created_by, deleted_at, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13)`,
		pipeline.ID, pipeline.TenantID, pipeline.Name, pipeline.DisplayName, pipeline.Description,
		string(pipeline.Type), []byte(pipeline.DAG), pipeline.Version, string(pipeline.Status),
		pipeline.CreatedBy, pipeline.DeletedAt, pipeline.CreatedAt, pipeline.UpdatedAt)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("create pipeline: %w", mapPostgresError(err))
	}
	return pipeline, nil
}

// GetPipeline loads one non-deleted definition scoped to tenant.
func (s *PostgresStore) GetPipeline(ctx context.Context, tenantID, pipelineID string) (domain.Pipeline, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+pipelineColumns+` FROM pipeline.pipelines
		WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL`, tenantID, pipelineID)
	pipeline, err := scanPipeline(row)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("get pipeline: %w", mapPostgresError(err))
	}
	return pipeline, nil
}

// ListPipelines returns a filtered page sorted by created_at DESC.
func (s *PostgresStore) ListPipelines(ctx context.Context, filter service.PipelineFilter) ([]domain.Pipeline, int, error) {
	where := []string{"tenant_id = $1::uuid", "deleted_at IS NULL"}
	args := []any{filter.TenantID}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.Type != "" {
		args = append(args, string(filter.Type))
		where = append(where, fmt.Sprintf("pipeline_type = $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pipeline.pipelines WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pipelines: %w", mapPostgresError(err))
	}
	limitIndex := len(args) + 1
	offsetIndex := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.PerPage, (filter.Page-1)*filter.PerPage)
	rows, err := s.pool.Query(ctx, `SELECT `+pipelineColumns+` FROM pipeline.pipelines WHERE `+clause+
		fmt.Sprintf(` ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d`, limitIndex, offsetIndex), listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list pipelines: %w", mapPostgresError(err))
	}
	defer rows.Close()
	items, err := collectPipelines(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list pipelines: %w", mapPostgresError(err))
	}
	return items, total, nil
}

// UpdatePipeline updates mutable definition fields.
func (s *PostgresStore) UpdatePipeline(ctx context.Context, pipeline domain.Pipeline) (domain.Pipeline, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE pipeline.pipelines
		SET display_name = $3, description = $4, dag = $5::jsonb, status = $6, updated_at = $7
		WHERE id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL
		RETURNING `+pipelineColumns,
		pipeline.ID, pipeline.TenantID, pipeline.DisplayName, pipeline.Description, []byte(pipeline.DAG),
		string(pipeline.Status), pipeline.UpdatedAt)
	updated, err := scanPipeline(row)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("update pipeline: %w", mapPostgresError(err))
	}
	return updated, nil
}

// DeletePipeline archives and soft-deletes a definition.
func (s *PostgresStore) DeletePipeline(ctx context.Context, tenantID, pipelineID string, deletedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE pipeline.pipelines
		SET deleted_at = $3, status = 'archived', updated_at = $3
		WHERE id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL`,
		pipelineID, tenantID, deletedAt)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", mapPostgresError(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipelineID)
	}
	return nil
}

// PublishPipeline atomically locks the definition, increments its version and
// inserts the immutable snapshot.
func (s *PostgresStore) PublishPipeline(ctx context.Context, pipeline domain.Pipeline, version domain.PipelineVersion) (domain.Pipeline, domain.PipelineVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("publish pipeline: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentVersion int
	var currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT version, status FROM pipeline.pipelines
		WHERE id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL
		FOR UPDATE`, pipeline.ID, pipeline.TenantID).Scan(&currentVersion, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipeline.ID)
	}
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("publish pipeline: %w", err)
	}
	if currentStatus == string(domain.PipelineStatusArchived) {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: archived pipeline cannot be published", domain.ErrInvalidState)
	}
	if pipeline.Version != currentVersion+1 {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: pipeline version changed concurrently", domain.ErrAlreadyExists)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE pipeline.pipelines SET version = $3, status = $4, updated_at = $5
		WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		pipeline.ID, pipeline.TenantID, pipeline.Version, string(pipeline.Status), pipeline.UpdatedAt); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("publish pipeline: %w", mapPostgresError(err))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO pipeline.versions
			(id, tenant_id, pipeline_id, version, dag, published_at, published_by, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::jsonb, $6, $7, $8, $9)`,
		version.ID, version.TenantID, version.PipelineID, version.Version, []byte(version.DAG),
		version.PublishedAt, version.PublishedBy, version.CreatedAt, version.UpdatedAt); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("insert pipeline version: %w", mapPostgresError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("publish pipeline: %w", err)
	}
	return pipeline, version, nil
}

// CreateRun inserts one run record.
func (s *PostgresStore) CreateRun(ctx context.Context, run domain.PipelineRun) (domain.PipelineRun, error) {
	_, err := s.pool.Exec(ctx, createRunSQL,
		run.ID, run.TenantID, run.PipelineID, run.Version, string(run.Status), string(run.TriggerType),
		run.ExecutionID, run.FailureCode, run.AttemptCount, run.LastReconciledAt,
		run.StartedAt, run.FinishedAt, run.Error, run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return domain.PipelineRun{}, fmt.Errorf("create pipeline run: %w", mapPostgresError(err))
	}
	return run, nil
}

// GetRun loads one run scoped to tenant and Pipeline.
func (s *PostgresStore) GetRun(ctx context.Context, tenantID, pipelineID, runID string) (domain.PipelineRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM pipeline.runs
		WHERE id = $1::uuid AND tenant_id = $2::uuid AND pipeline_id = $3::uuid`, runID, tenantID, pipelineID)
	run, err := scanRun(row)
	if err != nil {
		return domain.PipelineRun{}, fmt.Errorf("get pipeline run: %w", mapPostgresError(err))
	}
	return run, nil
}

// ListRuns returns runs sorted by started_at DESC.
func (s *PostgresStore) ListRuns(ctx context.Context, filter service.RunFilter) ([]domain.PipelineRun, int, error) {
	where := []string{"tenant_id = $1::uuid", "pipeline_id = $2::uuid"}
	args := []any{filter.TenantID, filter.PipelineID}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pipeline.runs WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pipeline runs: %w", mapPostgresError(err))
	}
	listArgs := append(append([]any{}, args...), filter.PerPage, (filter.Page-1)*filter.PerPage)
	rows, err := s.pool.Query(ctx, `
		SELECT `+runColumns+`
		FROM pipeline.runs WHERE `+clause+
		fmt.Sprintf(` ORDER BY COALESCE(started_at, created_at) DESC, id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list pipeline runs: %w", mapPostgresError(err))
	}
	defer rows.Close()
	items, err := collectRuns(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list pipeline runs: %w", mapPostgresError(err))
	}
	return items, total, nil
}

// UpdateRunConditional applies a forward-only run write with a compare-and-set
// guard: only runs that are still queued or running may transition, so a late
// or replayed reconciliation can never move a terminal run (D7). Nullable
// execution references and the attempt counter merge monotonically; a nil
// started_at/finished_at/last_reconciled_at or empty error keeps the stored
// value. A run that is missing, belongs to another tenant or is already
// terminal yields ErrInvalidState.
func (s *PostgresStore) UpdateRunConditional(ctx context.Context, run domain.PipelineRun) (domain.PipelineRun, error) {
	row := s.pool.QueryRow(ctx, updateRunConditionalSQL,
		run.TenantID, run.ID, string(run.Status), run.ExecutionID, run.FailureCode,
		run.AttemptCount, run.LastReconciledAt, run.StartedAt, run.FinishedAt,
		run.Error, run.UpdatedAt)
	updated, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.PipelineRun{}, fmt.Errorf("%w: pipeline run %s is not queued or running", domain.ErrInvalidState, run.ID)
		}
		return domain.PipelineRun{}, fmt.Errorf("update pipeline run conditionally: %w", mapPostgresError(err))
	}
	return updated, nil
}

// CreateRunInputs appends the declared input datasets of a run. The write is
// idempotent per (run_id, dataset_id), so a replayed trigger cannot duplicate
// lineage rows (D10, task 9.4).
func (s *PostgresStore) CreateRunInputs(ctx context.Context, inputs []domain.PipelineRunInput) error {
	if len(inputs) == 0 {
		return nil
	}
	if err := s.insertRunRows(ctx, "create pipeline run inputs", func(tx pgx.Tx) error {
		for _, input := range inputs {
			if _, err := tx.Exec(ctx, insertRunInputSQL,
				input.PipelineRunID, input.TenantID, input.DatasetID, input.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// CreateRunOutputs appends the dataset outputs of a succeeded run. The relation
// columns store the fully qualified physical table (catalog, schema, table).
// The write is idempotent per (run_id, dataset_id) so replaying a terminal
// callback cannot duplicate outputs (D10, task 9.4).
func (s *PostgresStore) CreateRunOutputs(ctx context.Context, outputs []domain.PipelineRunOutput) error {
	if len(outputs) == 0 {
		return nil
	}
	if err := s.insertRunRows(ctx, "create pipeline run outputs", func(tx pgx.Tx) error {
		for _, output := range outputs {
			if _, err := tx.Exec(ctx, insertRunOutputSQL,
				output.PipelineRunID, output.TenantID, output.DatasetID, output.Model,
				output.Catalog, output.Schema, output.Table, output.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// insertRunRows runs one lineage write as an atomic transaction. The callback
// only issues INSERT ... ON CONFLICT DO NOTHING statements.
func (s *PostgresStore) insertRunRows(ctx context.Context, operation string, write func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := write(tx); err != nil {
		return fmt.Errorf("%s: %w", operation, mapPostgresError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

// CreateSchedule inserts a schedule.
func (s *PostgresStore) CreateSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pipeline.schedules
			(id, tenant_id, pipeline_id, cron, timezone, enabled, next_run_at, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9)`,
		schedule.ID, schedule.TenantID, schedule.PipelineID, schedule.Cron, schedule.Timezone,
		schedule.Enabled, schedule.NextRunAt, schedule.CreatedAt, schedule.UpdatedAt)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("create schedule: %w", mapPostgresError(err))
	}
	return schedule, nil
}

// GetSchedule loads one schedule scoped to tenant.
func (s *PostgresStore) GetSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, pipeline_id::text, cron, timezone, enabled, next_run_at, created_at, updated_at
		FROM pipeline.schedules WHERE id = $1::uuid AND tenant_id = $2::uuid`, scheduleID, tenantID)
	schedule, err := scanSchedule(row)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("get schedule: %w", mapPostgresError(err))
	}
	return schedule, nil
}

// ListSchedules returns a page sorted by created_at DESC.
func (s *PostgresStore) ListSchedules(ctx context.Context, filter service.ScheduleFilter) ([]domain.Schedule, int, error) {
	where := []string{"tenant_id = $1::uuid"}
	args := []any{filter.TenantID}
	if filter.PipelineID != "" {
		args = append(args, filter.PipelineID)
		where = append(where, fmt.Sprintf("pipeline_id = $%d::uuid", len(args)))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pipeline.schedules WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count schedules: %w", mapPostgresError(err))
	}
	listArgs := append(append([]any{}, args...), filter.PerPage, (filter.Page-1)*filter.PerPage)
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, tenant_id::text, pipeline_id::text, cron, timezone, enabled, next_run_at, created_at, updated_at
		FROM pipeline.schedules WHERE `+clause+
		fmt.Sprintf(` ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list schedules: %w", mapPostgresError(err))
	}
	defer rows.Close()
	items, err := collectSchedules(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list schedules: %w", mapPostgresError(err))
	}
	return items, total, nil
}

// UpdateSchedule updates cron, timezone, enabled and next_run_at.
func (s *PostgresStore) UpdateSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE pipeline.schedules
		SET cron = $3, timezone = $4, enabled = $5, next_run_at = $6, updated_at = $7
		WHERE id = $1::uuid AND tenant_id = $2::uuid
		RETURNING id::text, tenant_id::text, pipeline_id::text, cron, timezone, enabled, next_run_at, created_at, updated_at`,
		schedule.ID, schedule.TenantID, schedule.Cron, schedule.Timezone, schedule.Enabled, schedule.NextRunAt, schedule.UpdatedAt)
	updated, err := scanSchedule(row)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("update schedule: %w", mapPostgresError(err))
	}
	return updated, nil
}

// DeleteSchedule removes a schedule.
func (s *PostgresStore) DeleteSchedule(ctx context.Context, tenantID, scheduleID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pipeline.schedules WHERE id = $1::uuid AND tenant_id = $2::uuid`, scheduleID, tenantID)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", mapPostgresError(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: schedule %s", domain.ErrNotFound, scheduleID)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPipeline(row rowScanner) (domain.Pipeline, error) {
	var pipeline domain.Pipeline
	var pipelineType, status string
	var dag []byte
	err := row.Scan(&pipeline.ID, &pipeline.TenantID, &pipeline.Name, &pipeline.DisplayName, &pipeline.Description,
		&pipelineType, &dag, &pipeline.Version, &status, &pipeline.CreatedBy, &pipeline.DeletedAt,
		&pipeline.CreatedAt, &pipeline.UpdatedAt)
	if err != nil {
		return domain.Pipeline{}, err
	}
	pipeline.DAG = json.RawMessage(dag)
	pipeline.Type = domain.PipelineType(pipelineType)
	pipeline.Status = domain.PipelineStatus(status)
	return pipeline, nil
}

func scanRun(row rowScanner) (domain.PipelineRun, error) {
	var run domain.PipelineRun
	var status, triggerType string
	err := row.Scan(&run.ID, &run.TenantID, &run.PipelineID, &run.Version, &status, &triggerType,
		&run.ExecutionID, &run.FailureCode, &run.AttemptCount, &run.LastReconciledAt,
		&run.StartedAt, &run.FinishedAt, &run.Error, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	run.Status = domain.PipelineRunStatus(status)
	run.TriggerType = domain.TriggerType(triggerType)
	return run, nil
}

func scanSchedule(row rowScanner) (domain.Schedule, error) {
	var schedule domain.Schedule
	err := row.Scan(&schedule.ID, &schedule.TenantID, &schedule.PipelineID, &schedule.Cron, &schedule.Timezone,
		&schedule.Enabled, &schedule.NextRunAt, &schedule.CreatedAt, &schedule.UpdatedAt)
	if err != nil {
		return domain.Schedule{}, err
	}
	return schedule, nil
}

func collectPipelines(rows pgx.Rows) ([]domain.Pipeline, error) {
	items := make([]domain.Pipeline, 0)
	for rows.Next() {
		item, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func collectRuns(rows pgx.Rows) ([]domain.PipelineRun, error) {
	items := make([]domain.PipelineRun, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func collectSchedules(rows pgx.Rows) ([]domain.Schedule, error) {
	items := make([]domain.Schedule, 0)
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// mapPostgresError translates SQLSTATE errors into domain sentinels while
// preserving the underlying error for logs.
func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", domain.ErrAlreadyExists, pgErr.Message)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidArgument, pgErr.Message)
		case "23514": // check_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidArgument, pgErr.Message)
		}
	}
	return err
}

// MarkSucceededWithOutputs writes the terminal run state and its output lineage
// in one transaction: either both are observable or neither is.
func (s *PostgresStore) MarkSucceededWithOutputs(ctx context.Context, run domain.PipelineRun, outputs []domain.PipelineRunOutput) (domain.PipelineRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PipelineRun{}, fmt.Errorf("mark run succeeded with outputs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	updated, err := scanRun(tx.QueryRow(ctx, updateRunConditionalSQL,
		run.TenantID, run.ID, string(run.Status), run.ExecutionID, run.FailureCode,
		run.AttemptCount, run.LastReconciledAt, run.StartedAt, run.FinishedAt,
		run.Error, run.UpdatedAt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.PipelineRun{}, fmt.Errorf("%w: pipeline run %s is not queued or running", domain.ErrInvalidState, run.ID)
		}
		return domain.PipelineRun{}, fmt.Errorf("mark run succeeded with outputs: %w", mapPostgresError(err))
	}
	for _, output := range outputs {
		if _, err := tx.Exec(ctx, insertRunOutputSQL,
			output.PipelineRunID, output.TenantID, output.DatasetID, output.Model,
			output.Catalog, output.Schema, output.Table, output.CreatedAt); err != nil {
			return domain.PipelineRun{}, fmt.Errorf("mark run succeeded with outputs: %w", mapPostgresError(err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PipelineRun{}, fmt.Errorf("mark run succeeded with outputs: %w", err)
	}
	return updated, nil
}
