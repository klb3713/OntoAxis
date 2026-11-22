-- +goose Up
-- +goose StatementBegin
CREATE SCHEMA IF NOT EXISTS object_data;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS object_data.objects (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    object_type_id uuid NOT NULL,
    properties jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1,
    owner_id text NOT NULL DEFAULT '',
    markings text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS objects_tenant_type_created_idx
    ON object_data.objects (tenant_id, object_type_id, created_at DESC) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS objects_properties_gin
    ON object_data.objects USING gin (properties jsonb_path_ops);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS object_data.object_links (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    link_type_id uuid NOT NULL,
    source_id uuid NOT NULL,
    target_id uuid NOT NULL,
    properties jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS object_links_unique_active_idx
    ON object_data.object_links (tenant_id, link_type_id, source_id, target_id)
    WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS object_links_source_idx
    ON object_data.object_links (tenant_id, source_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS object_links_target_idx
    ON object_data.object_links (tenant_id, target_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS object_data.object_links;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS object_data.objects;
-- +goose StatementEnd
