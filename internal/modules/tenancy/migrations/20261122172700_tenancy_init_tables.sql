-- +goose Up
-- +goose StatementBegin
CREATE SCHEMA IF NOT EXISTS core;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS core.organizations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    slug text NOT NULL,
    owner_id text NOT NULL DEFAULT '',
    parent_id uuid NULL REFERENCES core.organizations(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS organizations_tenant_slug_uq
    ON core.organizations (tenant_id, lower(slug)) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS organizations_tenant_idx ON core.organizations (tenant_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS core.workspaces (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    organization_id uuid NOT NULL REFERENCES core.organizations(id),
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    slug text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS workspaces_tenant_org_slug_uq
    ON core.workspaces (tenant_id, organization_id, lower(slug)) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS workspaces_tenant_org_idx ON core.workspaces (tenant_id, organization_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS core.memberships (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    principal_type text NOT NULL CHECK (principal_type IN ('user', 'group', 'service')),
    principal_id text NOT NULL,
    organization_id uuid NULL REFERENCES core.organizations(id),
    workspace_id uuid NULL REFERENCES core.workspaces(id),
    role text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'invited', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (organization_id IS NOT NULL OR workspace_id IS NOT NULL)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS memberships_tenant_org_idx ON core.memberships (tenant_id, organization_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS memberships_tenant_workspace_idx ON core.memberships (tenant_id, workspace_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS core.projects (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    organization_id uuid NOT NULL REFERENCES core.organizations(id),
    workspace_id uuid NOT NULL REFERENCES core.workspaces(id),
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS projects_tenant_org_name_uq
    ON core.projects (tenant_id, organization_id, name) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS projects_tenant_workspace_idx ON core.projects (tenant_id, workspace_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS core.project_resources (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES core.projects(id),
    resource_type text NOT NULL CHECK (resource_type IN ('object_type', 'dataset', 'pipeline', 'dashboard')),
    resource_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, resource_type, resource_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS project_resources_project_idx ON core.project_resources (tenant_id, project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS core.project_resources;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS core.projects;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS core.memberships;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS core.workspaces;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS core.organizations;
-- +goose StatementEnd
