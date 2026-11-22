-- +goose Up
CREATE SCHEMA IF NOT EXISTS jobs;

CREATE TABLE IF NOT EXISTS jobs.outbox (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_outbox_published_created
    ON jobs.outbox (published_at, created_at);

CREATE TABLE IF NOT EXISTS jobs.operations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    kind text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
    progress_percent integer NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    error text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_jobs_operations_tenant_status_created
    ON jobs.operations (tenant_id, status, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS jobs.operations;
DROP TABLE IF EXISTS jobs.outbox;
DROP SCHEMA IF EXISTS jobs;
