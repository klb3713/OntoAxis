-- +goose Up
CREATE SCHEMA IF NOT EXISTS query;

CREATE TABLE IF NOT EXISTS query.saved_queries (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    sql text NOT NULL,
    owner_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT saved_queries_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT saved_queries_sql_not_blank CHECK (btrim(sql) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS saved_queries_tenant_name_uniq
    ON query.saved_queries (tenant_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS saved_queries_tenant_created_idx
    ON query.saved_queries (tenant_id, created_at DESC)
    WHERE deleted_at IS NULL;

ALTER TABLE query.saved_queries ENABLE ROW LEVEL SECURITY;
ALTER TABLE query.saved_queries FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS saved_queries_tenant_isolation ON query.saved_queries;
CREATE POLICY saved_queries_tenant_isolation ON query.saved_queries
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE IF EXISTS query.saved_queries;
DROP SCHEMA IF EXISTS query;
