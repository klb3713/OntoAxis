package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

// Session settings consumed by the ingestion RLS policies. Both are scoped to
// a single transaction, so they never leak between pooled connections.
const (
	tenantSettingGUC   = "app.tenant_id"
	platformAdminGUC   = "app.is_platform_admin"
	platformAdminValue = "true"

	// maxRunningBatch caps how many running jobs one reconcile scan may load.
	maxRunningBatch = 500
)

// ClaimQueued atomically leases the oldest queued job whose lease is absent or
// expired and returns it. It runs under the platform-admin policy because the
// dispatcher scans jobs across all tenants. When no job is dispatchable it
// returns domain.ErrNotFound.
func (s *PostgresStore) ClaimQueued(ctx context.Context, leaseOwner string, leaseUntil time.Time) (*domain.SyncJob, error) {
	if strings.TrimSpace(leaseOwner) == "" {
		return nil, fmt.Errorf("ingestion claim lease owner is empty")
	}
	var claimed *domain.SyncJob
	err := s.withPlatformTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `UPDATE ingestion.sync_jobs
			SET lease_owner = $1, lease_until = $2, updated_at = now()
			WHERE id = (
				SELECT id FROM ingestion.sync_jobs
				WHERE status = 'queued' AND (lease_until IS NULL OR lease_until < now())
				ORDER BY created_at ASC, id ASC
				LIMIT 1
				FOR UPDATE SKIP LOCKED
			)
			RETURNING `+jobColumns, leaseOwner, leaseUntil)
		job, err := scanJob(row)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("claim queued sync job: %w", err)
		}
		claimed = job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// RenewLease extends the lease of a queued job still owned by leaseOwner. It
// returns domain.ErrConflict when the job moved on or the lease expired and was
// taken over, which tells the dispatcher to stop before submitting.
func (s *PostgresStore) RenewLease(ctx context.Context, jobID, leaseOwner string, leaseUntil time.Time) error {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(leaseOwner) == "" {
		return fmt.Errorf("ingestion lease id and owner are required")
	}
	return s.withPlatformTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET lease_until = $3, updated_at = now()
			WHERE id = $1 AND status = 'queued' AND lease_owner = $2`,
			jobID, leaseOwner, leaseUntil)
		if err != nil {
			return fmt.Errorf("renew sync job lease: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: sync job lease for %s is no longer held", domain.ErrConflict, jobID)
		}
		return nil
	})
}

// ReleaseLease clears the lease when leaseOwner still holds it. It is
// idempotent so dispatch cleanup can never fail a cycle.
func (s *PostgresStore) ReleaseLease(ctx context.Context, jobID, leaseOwner string) error {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(leaseOwner) == "" {
		return fmt.Errorf("ingestion lease id and owner are required")
	}
	return s.withPlatformTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET lease_owner = NULL, lease_until = NULL, updated_at = now()
			WHERE id = $1 AND lease_owner = $2`, jobID, leaseOwner); err != nil {
			return fmt.Errorf("release sync job lease: %w", err)
		}
		return nil
	})
}

// MarkRunningRuntime records a successful runtime submission: the job becomes
// running with its runtime reference and the run projection starts
// snapshotting. It fails with domain.ErrConflict when the job is no longer
// queued (for example because it was cancelled meanwhile).
func (s *PostgresStore) MarkRunningRuntime(ctx context.Context, job *domain.SyncJob, runtimeRef string, attempt int) error {
	if job == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.TenantID) == "" {
		return fmt.Errorf("mark running requires a persisted sync job")
	}
	if strings.TrimSpace(runtimeRef) == "" {
		return fmt.Errorf("mark running requires a runtime reference")
	}
	stats := map[string]any{
		domain.StatsPhase:      domain.PhaseSnapshotting,
		domain.StatsRuntimeRef: runtimeRef,
		domain.StatsAttempt:    attempt,
	}
	return s.withTenantTx(ctx, job.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET status = 'running', runtime_ref = $3, attempt = $4,
				lease_owner = NULL, lease_until = NULL,
				started_at = COALESCE(started_at, now()), updated_at = now()
			WHERE tenant_id = $1 AND id = $2 AND status = 'queued'`,
			job.TenantID, job.ID, runtimeRef, attempt)
		if err != nil {
			return fmt.Errorf("mark sync job running: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return s.markConflict(ctx, tx, job.TenantID, job.ID, "queued")
		}
		return updateLatestRunTx(ctx, tx, job.TenantID, job.ID, domain.StatusRunning, stats, "", false)
	})
}

// RecordAttempt persists a retryable submission failure: the job stays queued
// but records the consumed attempt, the redacted error and progress stats.
func (s *PostgresStore) RecordAttempt(ctx context.Context, tenantID, jobID string, attempt int, message string, stats map[string]any) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("record attempt requires tenant and job ids")
	}
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET attempt = $3, updated_at = now()
			WHERE tenant_id = $1 AND id = $2 AND status = 'queued'`,
			tenantID, jobID, attempt)
		if err != nil {
			return fmt.Errorf("record sync job attempt: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return s.markConflict(ctx, tx, tenantID, jobID, "queued")
		}
		return updateLatestRunTx(ctx, tx, tenantID, jobID, domain.StatusQueued, stats, message, false)
	})
}

// MarkJobFailed moves a queued or running job to failed and records the redacted
// error on the latest run. Terminal jobs are left untouched: the conflict or
// not-found result tells the caller to stop.
func (s *PostgresStore) MarkJobFailed(ctx context.Context, tenantID, jobID, message string, stats map[string]any) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("mark failed requires tenant and job ids")
	}
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET status = 'failed', lease_owner = NULL, lease_until = NULL,
				finished_at = COALESCE(finished_at, now()), updated_at = now()
			WHERE tenant_id = $1 AND id = $2 AND status IN ('queued','running')`,
			tenantID, jobID)
		if err != nil {
			return fmt.Errorf("mark sync job failed: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return s.markConflict(ctx, tx, tenantID, jobID, "queued or running")
		}
		return updateLatestRunTx(ctx, tx, tenantID, jobID, domain.StatusFailed, stats, message, true)
	})
}

// MarkJobSucceeded moves a running job to succeeded and records verification
// stats on the latest run. Only a running job may succeed.
func (s *PostgresStore) MarkJobSucceeded(ctx context.Context, tenantID, jobID string, stats map[string]any) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("mark succeeded requires tenant and job ids")
	}
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ingestion.sync_jobs
			SET status = 'succeeded', lease_owner = NULL, lease_until = NULL,
				finished_at = COALESCE(finished_at, now()), updated_at = now()
			WHERE tenant_id = $1 AND id = $2 AND status = 'running'`,
			tenantID, jobID)
		if err != nil {
			return fmt.Errorf("mark sync job succeeded: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return s.markConflict(ctx, tx, tenantID, jobID, "running")
		}
		return updateLatestRunTx(ctx, tx, tenantID, jobID, domain.StatusSucceeded, stats, "", true)
	})
}

// ListRunning returns up to limit running jobs across tenants, oldest update
// first, for reconciliation. It requires jobs to have a runtime reference; the
// reconciler skips those that do not.
func (s *PostgresStore) ListRunning(ctx context.Context, limit int) ([]domain.SyncJob, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("list running limit must be positive")
	}
	if limit > maxRunningBatch {
		limit = maxRunningBatch
	}
	jobs := make([]domain.SyncJob, 0, limit)
	err := s.withPlatformTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM ingestion.sync_jobs
			WHERE status = 'running'
			ORDER BY updated_at ASC, created_at ASC, id ASC
			LIMIT $1`, limit)
		if err != nil {
			return fmt.Errorf("list running sync jobs: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, *job)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate running sync jobs: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// SetJobRuntimeStats merges stats into the latest run projection without
// changing the run status or error. The reconciler uses it for progress and
// cleanup-pending markers.
func (s *PostgresStore) SetJobRuntimeStats(ctx context.Context, tenantID, jobID string, stats map[string]any) error {
	if len(stats) == 0 {
		return nil
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("set runtime stats requires tenant and job ids")
	}
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return updateLatestRunTx(ctx, tx, tenantID, jobID, "", stats, "", false)
	})
}

// UpdateJobStats is the general form of SetJobRuntimeStats: it merges stats
// into the latest run projection without changing its status. Both names are
// kept because the runtime port calls the narrower one and the repo API had
// already promised UpdateJobStats.
func (s *PostgresStore) UpdateJobStats(ctx context.Context, tenantID, jobID string, stats map[string]any) error {
	return s.SetJobRuntimeStats(ctx, tenantID, jobID, stats)
}

// FindActiveJobByConnector returns the oldest queued or running job for a
// connector, excluding excludeJobID when it is not empty. The dispatcher uses it
// to reject a second concurrent sync of the same connector.
func (s *PostgresStore) FindActiveJobByConnector(ctx context.Context, tenantID, connectorID, excludeJobID string) (*domain.SyncJob, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(connectorID) == "" {
		return nil, fmt.Errorf("find active job requires tenant and connector ids")
	}
	query := `SELECT ` + jobColumns + ` FROM ingestion.sync_jobs
		WHERE tenant_id = $1 AND connector_id = $2 AND status IN ('queued','running')`
	args := []any{tenantID, connectorID}
	if excludeJobID != "" {
		if _, err := uuid.Parse(excludeJobID); err != nil {
			return nil, fmt.Errorf("find active job: invalid exclude job id: %w", err)
		}
		query += ` AND id <> $3`
		args = append(args, excludeJobID)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT 1`
	var found *domain.SyncJob
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		job, err := scanJob(tx.QueryRow(ctx, query, args...))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("find active sync job: %w", err)
		}
		found = job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// withPlatformTx runs fn in a transaction where the platform-admin RLS policy
// applies. Only the dispatcher and reconciler scans use it.
func (s *PostgresStore) withPlatformTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.withSettingTx(ctx, platformAdminGUC, platformAdminValue, fn)
}

// withTenantTx runs fn in a transaction scoped to a single tenant.
func (s *PostgresStore) withTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("ingestion tenant id is empty")
	}
	return s.withSettingTx(ctx, tenantSettingGUC, tenantID, fn)
}

// withSettingTx begins a transaction, applies one local session setting, runs
// fn and commits. A rollback is always scheduled and is a no-op after commit.
func (s *PostgresStore) withSettingTx(ctx context.Context, setting, value string, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ingestion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, setting, value); err != nil {
		return fmt.Errorf("set ingestion transaction setting %s: %w", setting, err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ingestion transaction: %w", err)
	}
	return nil
}

// markConflict distinguishes a missing job from a job in an unexpected status.
// It is called after a guarded UPDATE affected no rows.
func (s *PostgresStore) markConflict(ctx context.Context, tx pgx.Tx, tenantID, jobID, expected string) error {
	var status domain.JobStatus
	err := tx.QueryRow(ctx, `SELECT status FROM ingestion.sync_jobs WHERE tenant_id = $1 AND id = $2`,
		tenantID, jobID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: sync job %s", domain.ErrNotFound, jobID)
		}
		return fmt.Errorf("load sync job status: %w", err)
	}
	return fmt.Errorf("%w: sync job %s is %s, expected %s", domain.ErrConflict, jobID, status, expected)
}

// updateLatestRunTx merges stats into the latest run projection. When status is
// empty the run status and error are left untouched; terminal=true stamps
// finished_at. A missing run reports domain.ErrNotFound.
func updateLatestRunTx(ctx context.Context, tx pgx.Tx, tenantID, jobID string, status domain.JobStatus, stats map[string]any, message string, terminal bool) error {
	encoded, err := marshalMap(stats)
	if err != nil {
		return err
	}
	const latestRun = `(SELECT id FROM ingestion.sync_runs
		WHERE tenant_id = $1 AND job_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1)`
	var tag pgconn.CommandTag
	if status == "" {
		tag, err = tx.Exec(ctx, `UPDATE ingestion.sync_runs
			SET stats = stats || $3::jsonb, updated_at = now(),
				finished_at = CASE WHEN $4 THEN COALESCE(finished_at, now()) ELSE finished_at END
			WHERE tenant_id = $1 AND id = `+latestRun,
			tenantID, jobID, encoded, terminal)
	} else {
		tag, err = tx.Exec(ctx, `UPDATE ingestion.sync_runs
			SET status = $3, stats = stats || $4::jsonb, error = $5, updated_at = now(),
				started_at = CASE WHEN $3 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
				finished_at = CASE WHEN $6 THEN COALESCE(finished_at, now()) ELSE finished_at END
			WHERE tenant_id = $1 AND id = `+latestRun,
			tenantID, jobID, status, encoded, message, terminal)
	}
	if err != nil {
		return fmt.Errorf("update sync run projection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: sync run for job %s", domain.ErrNotFound, jobID)
	}
	return nil
}
