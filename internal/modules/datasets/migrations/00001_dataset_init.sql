-- dataset catalog module schema. Every statement is idempotent so that
-- Module.Migrate can be retried without a migration ledger.
CREATE SCHEMA IF NOT EXISTS dataset;

CREATE TABLE IF NOT EXISTS dataset.datasets (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name varchar(128) NOT NULL,
    display_name varchar(255) NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    rid varchar(255) NOT NULL,
    format varchar(32) NOT NULL,
    storage_location text NOT NULL DEFAULT '',
    owner_id uuid,
    tags jsonb NOT NULL DEFAULT '{}'::jsonb,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT datasets_name_not_empty CHECK (length(btrim(name)) > 0),
    CONSTRAINT datasets_format_check CHECK (format IN ('iceberg', 'parquet')),
    CONSTRAINT datasets_tenant_name_key UNIQUE (tenant_id, name),
    CONSTRAINT datasets_rid_key UNIQUE (rid)
);

CREATE INDEX IF NOT EXISTS datasets_tenant_created_idx ON dataset.datasets (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS dataset.dataset_branches (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    name varchar(128) NOT NULL,
    parent_branch_id uuid REFERENCES dataset.dataset_branches (id),
    head_transaction_id uuid,
    created_from_transaction_id uuid,
    fallback_chain jsonb NOT NULL DEFAULT '[]'::jsonb,
    labels jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dataset_branches_name_not_empty CHECK (length(btrim(name)) > 0),
    CONSTRAINT dataset_branches_tenant_dataset_name_key UNIQUE (tenant_id, dataset_id, name)
);

CREATE INDEX IF NOT EXISTS dataset_branches_tenant_dataset_idx ON dataset.dataset_branches (tenant_id, dataset_id);

CREATE TABLE IF NOT EXISTS dataset.dataset_transactions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    branch_id uuid NOT NULL REFERENCES dataset.dataset_branches (id),
    transaction_type varchar(32) NOT NULL DEFAULT 'snapshot',
    status varchar(16) NOT NULL DEFAULT 'open',
    message text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    committed_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dataset_transactions_type_check CHECK (transaction_type IN ('snapshot', 'append', 'update', 'delete')),
    CONSTRAINT dataset_transactions_status_check CHECK (status IN ('open', 'committed', 'aborted'))
);

CREATE INDEX IF NOT EXISTS dataset_transactions_tenant_dataset_branch_idx
    ON dataset.dataset_transactions (tenant_id, dataset_id, branch_id, started_at DESC);

CREATE TABLE IF NOT EXISTS dataset.dataset_views (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    branch_id uuid REFERENCES dataset.dataset_branches (id),
    name varchar(128) NOT NULL,
    query text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dataset_views_name_not_empty CHECK (length(btrim(name)) > 0),
    CONSTRAINT dataset_views_query_not_empty CHECK (length(btrim(query)) > 0),
    CONSTRAINT dataset_views_version_positive CHECK (version > 0),
    CONSTRAINT dataset_views_tenant_dataset_name_key UNIQUE (tenant_id, dataset_id, name)
);

CREATE INDEX IF NOT EXISTS dataset_views_tenant_dataset_idx ON dataset.dataset_views (tenant_id, dataset_id);

CREATE TABLE IF NOT EXISTS dataset.dataset_permissions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    principal_type varchar(16) NOT NULL,
    principal_id uuid NOT NULL,
    permission varchar(32) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dataset_permissions_principal_type_check CHECK (principal_type IN ('user', 'group', 'service')),
    CONSTRAINT dataset_permissions_key UNIQUE (tenant_id, dataset_id, principal_type, principal_id, permission)
);

CREATE TABLE IF NOT EXISTS dataset.quality_rules (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    name varchar(128) NOT NULL,
    rule_type varchar(64) NOT NULL,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT quality_rules_tenant_dataset_name_key UNIQUE (tenant_id, dataset_id, name)
);

CREATE TABLE IF NOT EXISTS dataset.retention_policies (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    ttl_days integer NOT NULL DEFAULT 0,
    archive_after_days integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT retention_policies_tenant_dataset_key UNIQUE (tenant_id, dataset_id)
);

CREATE TABLE IF NOT EXISTS dataset.pipeline_bindings (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    pipeline_id uuid NOT NULL,
    direction varchar(16) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pipeline_bindings_direction_check CHECK (direction IN ('source', 'sink')),
    CONSTRAINT pipeline_bindings_key UNIQUE (tenant_id, dataset_id, pipeline_id, direction)
);

CREATE TABLE IF NOT EXISTS dataset.dataset_files (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL REFERENCES dataset.datasets (id),
    branch_id uuid REFERENCES dataset.dataset_branches (id),
    object_key text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    row_count bigint NOT NULL DEFAULT 0,
    etag varchar(128) NOT NULL DEFAULT '',
    content_type varchar(255) NOT NULL DEFAULT '',
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dataset_files_object_key_not_empty CHECK (length(btrim(object_key)) > 0),
    CONSTRAINT dataset_files_size_non_negative CHECK (size_bytes >= 0),
    CONSTRAINT dataset_files_row_count_non_negative CHECK (row_count >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS dataset_files_tenant_dataset_object_key_idx
    ON dataset.dataset_files (tenant_id, dataset_id, object_key)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS dataset_files_tenant_dataset_idx ON dataset.dataset_files (tenant_id, dataset_id);

ALTER TABLE dataset.datasets ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.datasets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS datasets_tenant_isolation ON dataset.datasets;
CREATE POLICY datasets_tenant_isolation ON dataset.datasets USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS datasets_platform_admin ON dataset.datasets;
CREATE POLICY datasets_platform_admin ON dataset.datasets USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.dataset_branches ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.dataset_branches FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dataset_branches_tenant_isolation ON dataset.dataset_branches;
CREATE POLICY dataset_branches_tenant_isolation ON dataset.dataset_branches USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS dataset_branches_platform_admin ON dataset.dataset_branches;
CREATE POLICY dataset_branches_platform_admin ON dataset.dataset_branches USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.dataset_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.dataset_transactions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dataset_transactions_tenant_isolation ON dataset.dataset_transactions;
CREATE POLICY dataset_transactions_tenant_isolation ON dataset.dataset_transactions USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS dataset_transactions_platform_admin ON dataset.dataset_transactions;
CREATE POLICY dataset_transactions_platform_admin ON dataset.dataset_transactions USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.dataset_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.dataset_views FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dataset_views_tenant_isolation ON dataset.dataset_views;
CREATE POLICY dataset_views_tenant_isolation ON dataset.dataset_views USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS dataset_views_platform_admin ON dataset.dataset_views;
CREATE POLICY dataset_views_platform_admin ON dataset.dataset_views USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.dataset_permissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.dataset_permissions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dataset_permissions_tenant_isolation ON dataset.dataset_permissions;
CREATE POLICY dataset_permissions_tenant_isolation ON dataset.dataset_permissions USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS dataset_permissions_platform_admin ON dataset.dataset_permissions;
CREATE POLICY dataset_permissions_platform_admin ON dataset.dataset_permissions USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.quality_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.quality_rules FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS quality_rules_tenant_isolation ON dataset.quality_rules;
CREATE POLICY quality_rules_tenant_isolation ON dataset.quality_rules USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS quality_rules_platform_admin ON dataset.quality_rules;
CREATE POLICY quality_rules_platform_admin ON dataset.quality_rules USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.retention_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.retention_policies FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS retention_policies_tenant_isolation ON dataset.retention_policies;
CREATE POLICY retention_policies_tenant_isolation ON dataset.retention_policies USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS retention_policies_platform_admin ON dataset.retention_policies;
CREATE POLICY retention_policies_platform_admin ON dataset.retention_policies USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.pipeline_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.pipeline_bindings FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pipeline_bindings_tenant_isolation ON dataset.pipeline_bindings;
CREATE POLICY pipeline_bindings_tenant_isolation ON dataset.pipeline_bindings USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS pipeline_bindings_platform_admin ON dataset.pipeline_bindings;
CREATE POLICY pipeline_bindings_platform_admin ON dataset.pipeline_bindings USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

ALTER TABLE dataset.dataset_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataset.dataset_files FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dataset_files_tenant_isolation ON dataset.dataset_files;
CREATE POLICY dataset_files_tenant_isolation ON dataset.dataset_files USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS dataset_files_platform_admin ON dataset.dataset_files;
CREATE POLICY dataset_files_platform_admin ON dataset.dataset_files USING (current_setting('app.is_platform_admin', true) = 'true') WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');
