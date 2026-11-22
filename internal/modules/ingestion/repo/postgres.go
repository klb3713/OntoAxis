// Package repo implements the ingestion Store port with PostgreSQL. Every
// statement is scoped by tenant_id; the package is not exported outside the
// ingestion module.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

const jobColumns = `id, tenant_id, connector_id, mode, status, config, created_at, updated_at, started_at, finished_at`
const runColumns = `id, tenant_id, job_id, status, stats, error, created_at, updated_at, started_at, finished_at`

// PostgresStore persists sync jobs and runs.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// New builds a store over an already-open pool.
func New(pool *pgxpool.Pool) *PostgresStore {
	return NewPostgresStore(pool)
}

// NewPostgresStore builds a store over an already-open pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// CreateJob inserts a job and its initial run atomically.
func (s *PostgresStore) CreateJob(ctx context.Context, job *domain.SyncJob, run *domain.SyncRun) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create sync job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	config, err := marshalMap(job.Config)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ingestion.sync_jobs
		(id, tenant_id, connector_id, mode, status, config, created_at, updated_at, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		job.ID, job.TenantID, job.ConnectorID, job.Mode, job.Status, config, job.CreatedAt, job.UpdatedAt, job.StartedAt, job.FinishedAt); err != nil {
		return fmt.Errorf("insert sync job: %w", err)
	}
	stats, err := marshalMap(run.Stats)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ingestion.sync_runs
		(id, tenant_id, job_id, status, stats, error, created_at, updated_at, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		run.ID, run.TenantID, run.JobID, run.Status, stats, run.Error, run.CreatedAt, run.UpdatedAt, run.StartedAt, run.FinishedAt); err != nil {
		return fmt.Errorf("insert sync run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create sync job: %w", err)
	}
	return nil
}

// GetJob returns a tenant-owned job.
func (s *PostgresStore) GetJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM ingestion.sync_jobs WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return job, nil
}

// ListJobs returns a tenant-scoped page, optionally filtered by connector.
func (s *PostgresStore) ListJobs(ctx context.Context, tenantID, connectorID string, limit, offset int32) ([]domain.SyncJob, int64, error) {
	where := `tenant_id = $1`
	args := []any{tenantID}
	if connectorID != "" {
		where += ` AND connector_id = $2`
		args = append(args, connectorID)
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingestion.sync_jobs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sync jobs: %w", err)
	}
	args = append(args, limit, offset)
	query := `SELECT ` + jobColumns + ` FROM ingestion.sync_jobs WHERE ` + where +
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sync jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]domain.SyncJob, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate sync jobs: %w", err)
	}
	return jobs, total, nil
}

// GetLatestRun returns the newest run for a tenant-owned job.
func (s *PostgresStore) GetLatestRun(ctx context.Context, tenantID, jobID string) (*domain.SyncRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM ingestion.sync_runs
		WHERE tenant_id = $1 AND job_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`, tenantID, jobID)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return run, nil
}

// UpdateJobAndRun updates the job (guarded by its expected status) and the run
// projection in one transaction.
func (s *PostgresStore) UpdateJobAndRun(ctx context.Context, job *domain.SyncJob, expectedStatus domain.JobStatus, run *domain.SyncRun) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update sync job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
		SET status = $3, started_at = $4, finished_at = $5, updated_at = $6
		WHERE tenant_id = $1 AND id = $2 AND status = $7`,
		job.TenantID, job.ID, job.Status, job.StartedAt, job.FinishedAt, job.UpdatedAt, expectedStatus)
	if err != nil {
		return fmt.Errorf("update sync job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: sync job %s is not %s", domain.ErrConflict, job.ID, expectedStatus)
	}
	stats, err := marshalMap(run.Stats)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE ingestion.sync_runs
		SET status = $3, stats = $4, error = $5, started_at = $6, finished_at = $7, updated_at = $8
		WHERE tenant_id = $1 AND id = $2`,
		run.TenantID, run.ID, run.Status, stats, run.Error, run.StartedAt, run.FinishedAt, run.UpdatedAt); err != nil {
		return fmt.Errorf("update sync run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update sync job: %w", err)
	}
	return nil
}

// ListRuns returns all run projections for a tenant-owned job.
func (s *PostgresStore) ListRuns(ctx context.Context, tenantID, jobID string) ([]domain.SyncRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM ingestion.sync_runs
		WHERE tenant_id = $1 AND job_id = $2 ORDER BY started_at DESC NULLS LAST, created_at DESC, id DESC`, tenantID, jobID)
	if err != nil {
		return nil, fmt.Errorf("list sync runs: %w", err)
	}
	defer rows.Close()
	runs := make([]domain.SyncRun, 0, 4)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync runs: %w", err)
	}
	return runs, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*domain.SyncJob, error) {
	var job domain.SyncJob
	var config []byte
	if err := row.Scan(&job.ID, &job.TenantID, &job.ConnectorID, &job.Mode, &job.Status, &config,
		&job.CreatedAt, &job.UpdatedAt, &job.StartedAt, &job.FinishedAt); err != nil {
		return nil, err
	}
	job.Config = unmarshalMap(config)
	return &job, nil
}

func scanRun(row rowScanner) (*domain.SyncRun, error) {
	var run domain.SyncRun
	var stats []byte
	var message *string
	if err := row.Scan(&run.ID, &run.TenantID, &run.JobID, &run.Status, &stats, &message,
		&run.CreatedAt, &run.UpdatedAt, &run.StartedAt, &run.FinishedAt); err != nil {
		return nil, err
	}
	if message != nil {
		run.Error = *message
	}
	run.Stats = unmarshalMap(stats)
	return &run, nil
}

func marshalMap(value map[string]any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json map: %w", err)
	}
	return encoded, nil
}

func unmarshalMap(value []byte) map[string]any {
	out := map[string]any{}
	if len(value) == 0 {
		return out
	}
	if err := json.Unmarshal(value, &out); err != nil {
		return map[string]any{}
	}
	return out
}
