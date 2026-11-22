// Package migrations holds the idempotent ingestion schema DDL.
package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/db"
)

// Statements creates the ingestion schema, job/run tables, indexes, and
// tenant RLS policies. Every statement is safe to execute repeatedly.
var Statements = []string{
	`CREATE SCHEMA IF NOT EXISTS ingestion`,
	`CREATE TABLE IF NOT EXISTS ingestion.sync_jobs (
		id uuid PRIMARY KEY,
		tenant_id uuid NOT NULL,
		connector_id uuid NOT NULL,
		mode text NOT NULL DEFAULT 'full',
		status text NOT NULL DEFAULT 'queued',
		config jsonb NOT NULL DEFAULT '{}'::jsonb,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now(),
		started_at timestamptz,
		finished_at timestamptz,
		CONSTRAINT sync_jobs_status_check CHECK (status IN ('queued','running','succeeded','failed','cancelled'))
	)`,
	// Additive dispatch fields. CREATE TABLE above intentionally keeps the
	// original shape so existing clusters can be upgraded in place.
	`ALTER TABLE ingestion.sync_jobs ADD COLUMN IF NOT EXISTS runtime_ref text`,
	`ALTER TABLE ingestion.sync_jobs ADD COLUMN IF NOT EXISTS attempt int NOT NULL DEFAULT 0`,
	`ALTER TABLE ingestion.sync_jobs ADD COLUMN IF NOT EXISTS lease_owner text`,
	`ALTER TABLE ingestion.sync_jobs ADD COLUMN IF NOT EXISTS lease_until timestamptz`,
	`CREATE INDEX IF NOT EXISTS sync_jobs_tenant_connector_created_idx
		ON ingestion.sync_jobs (tenant_id, connector_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS sync_jobs_dispatch_idx
		ON ingestion.sync_jobs (status, lease_until)`,
	`CREATE INDEX IF NOT EXISTS sync_jobs_tenant_connector_status_idx
		ON ingestion.sync_jobs (tenant_id, connector_id, status)`,
	`CREATE TABLE IF NOT EXISTS ingestion.sync_runs (
		id uuid PRIMARY KEY,
		tenant_id uuid NOT NULL,
		job_id uuid NOT NULL REFERENCES ingestion.sync_jobs(id) ON DELETE CASCADE,
		status text NOT NULL DEFAULT 'queued',
		stats jsonb NOT NULL DEFAULT '{}'::jsonb,
		error text NOT NULL DEFAULT '',
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now(),
		started_at timestamptz,
		finished_at timestamptz,
		CONSTRAINT sync_runs_status_check CHECK (status IN ('queued','running','succeeded','failed','cancelled'))
	)`,
	`CREATE INDEX IF NOT EXISTS sync_runs_tenant_job_started_idx
		ON ingestion.sync_runs (tenant_id, job_id, started_at DESC)`,
	`ALTER TABLE ingestion.sync_jobs ENABLE ROW LEVEL SECURITY`,
	`ALTER TABLE ingestion.sync_jobs FORCE ROW LEVEL SECURITY`,
	`ALTER TABLE ingestion.sync_runs ENABLE ROW LEVEL SECURITY`,
	`ALTER TABLE ingestion.sync_runs FORCE ROW LEVEL SECURITY`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'ingestion' AND tablename = 'sync_jobs' AND policyname = 'sync_jobs_tenant_isolation'
		) THEN
			CREATE POLICY sync_jobs_tenant_isolation ON ingestion.sync_jobs
				USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		END IF;
	END $$`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'ingestion' AND tablename = 'sync_jobs' AND policyname = 'sync_jobs_platform_admin'
		) THEN
			CREATE POLICY sync_jobs_platform_admin ON ingestion.sync_jobs
				USING (current_setting('app.is_platform_admin', true) = 'true')
				WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');
		END IF;
	END $$`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'ingestion' AND tablename = 'sync_runs' AND policyname = 'sync_runs_tenant_isolation'
		) THEN
			CREATE POLICY sync_runs_tenant_isolation ON ingestion.sync_runs
				USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		END IF;
	END $$`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'ingestion' AND tablename = 'sync_runs' AND policyname = 'sync_runs_platform_admin'
		) THEN
			CREATE POLICY sync_runs_platform_admin ON ingestion.sync_runs
				USING (current_setting('app.is_platform_admin', true) = 'true')
				WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');
		END IF;
	END $$`,
}

// DownStatements reverses Statements in dependency order. Dropping the tables
// also removes the additive runtime columns, so no extra ALTER is required.
var DownStatements = []string{
	`DROP TABLE IF EXISTS ingestion.sync_runs`,
	`DROP TABLE IF EXISTS ingestion.sync_jobs`,
	`DROP SCHEMA IF EXISTS ingestion`,
}

// Up applies the ingestion schema.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	return db.ExecStatements(ctx, pool, Statements...)
}

// Down drops the ingestion objects (used by explicit rollback tooling/tests).
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	return db.ExecStatements(ctx, pool, DownStatements...)
}
