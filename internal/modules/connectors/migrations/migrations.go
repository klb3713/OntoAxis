// Package migrations owns the connectors schema DDL. Statements are idempotent
// so Module.Migrate can be retried safely, and DownStatements document the
// matching rollback order.
package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/db"
)

// Statements creates the connector schema, table, indexes, and row-level
// security policies.
var Statements = []string{
	`CREATE SCHEMA IF NOT EXISTS connector`,
	`CREATE TABLE IF NOT EXISTS connector.connectors (
		id uuid PRIMARY KEY,
		tenant_id uuid NOT NULL,
		name text NOT NULL,
		display_name text NOT NULL DEFAULT '',
		connector_type text NOT NULL CHECK (connector_type IN ('postgres', 'mysql', 's3', 'rest_api', 'kafka')),
		config jsonb NOT NULL DEFAULT '{}'::jsonb,
		tags jsonb NOT NULL DEFAULT '[]'::jsonb,
		secret_ref text NOT NULL DEFAULT '',
		status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'error')),
		created_by uuid,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now(),
		deleted_at timestamptz
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS connectors_tenant_name_uq
		ON connector.connectors (tenant_id, name)
		WHERE deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS connectors_tenant_created_idx
		ON connector.connectors (tenant_id, created_at DESC)`,
	`ALTER TABLE connector.connectors ENABLE ROW LEVEL SECURITY`,
	`ALTER TABLE connector.connectors FORCE ROW LEVEL SECURITY`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'connector' AND tablename = 'connectors'
			  AND policyname = 'connectors_tenant_isolation'
		) THEN
			CREATE POLICY connectors_tenant_isolation ON connector.connectors
				USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		END IF;
	END
	$$`,
	`DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = 'connector' AND tablename = 'connectors'
			  AND policyname = 'connectors_platform_admin'
		) THEN
			CREATE POLICY connectors_platform_admin ON connector.connectors
				USING (current_setting('app.is_platform_admin', true) = 'true')
				WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');
		END IF;
	END
	$$`,
}

// DownStatements drops policies, table, and schema in reverse creation order.
var DownStatements = []string{
	`DROP POLICY IF EXISTS connectors_tenant_isolation ON connector.connectors`,
	`DROP POLICY IF EXISTS connectors_platform_admin ON connector.connectors`,
	`DROP TABLE IF EXISTS connector.connectors`,
	`DROP SCHEMA IF EXISTS connector`,
}

// Up applies the idempotent connector DDL through the platform helper.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	return db.ExecStatements(ctx, pool, Statements...)
}

// Down applies the rollback statements.
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	return db.ExecStatements(ctx, pool, DownStatements...)
}
