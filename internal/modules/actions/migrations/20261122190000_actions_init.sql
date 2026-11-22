-- +goose Up
-- +goose StatementBegin
CREATE SCHEMA IF NOT EXISTS action;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS action.action_types (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL,
    description text NOT NULL DEFAULT '',
    object_type_id uuid NULL,
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS action_types_unique_active_idx
    ON action.action_types (tenant_id, name) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS action_types_tenant_created_idx
    ON action.action_types (tenant_id, created_at DESC) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS action.action_executions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    action_type_id uuid NOT NULL REFERENCES action.action_types(id),
    status text NOT NULL DEFAULT 'queued',
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    result jsonb NULL,
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz NULL,
    finished_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS action_executions_tenant_type_created_idx
    ON action.action_executions (tenant_id, action_type_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS action.action_executions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS action.action_types;
-- +goose StatementEnd
