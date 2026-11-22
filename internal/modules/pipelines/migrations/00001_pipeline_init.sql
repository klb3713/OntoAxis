-- +statement
CREATE SCHEMA IF NOT EXISTS pipeline;

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.pipelines (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    pipeline_type text NOT NULL,
    dag jsonb NOT NULL DEFAULT '{}'::jsonb,
    version integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'draft',
    created_by text NOT NULL DEFAULT '',
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pipelines_tenant_name_key UNIQUE (tenant_id, name),
    CONSTRAINT pipelines_pipeline_type_check CHECK (pipeline_type IN ('batch', 'faster', 'incremental')),
    CONSTRAINT pipelines_status_check CHECK (status IN ('draft', 'published', 'archived'))
);

-- +statement
CREATE INDEX IF NOT EXISTS pipelines_tenant_updated_idx
    ON pipeline.pipelines (tenant_id, updated_at DESC);

-- +statement
CREATE INDEX IF NOT EXISTS pipelines_tenant_created_idx
    ON pipeline.pipelines (tenant_id, created_at DESC);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.versions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    pipeline_id uuid NOT NULL REFERENCES pipeline.pipelines (id),
    version integer NOT NULL,
    dag jsonb NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    published_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT versions_tenant_pipeline_version_key UNIQUE (tenant_id, pipeline_id, version)
);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.runs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    pipeline_id uuid NOT NULL REFERENCES pipeline.pipelines (id),
    version integer NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    trigger_type text NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT runs_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT runs_trigger_type_check CHECK (trigger_type IN ('manual', 'schedule'))
);

-- +statement
CREATE INDEX IF NOT EXISTS runs_tenant_pipeline_started_idx
    ON pipeline.runs (tenant_id, pipeline_id, started_at DESC);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.schedules (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    pipeline_id uuid NOT NULL REFERENCES pipeline.pipelines (id),
    cron text NOT NULL,
    timezone text NOT NULL DEFAULT 'UTC',
    enabled boolean NOT NULL DEFAULT true,
    next_run_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT schedules_tenant_pipeline_cron_key UNIQUE (tenant_id, pipeline_id, cron)
);

-- +statement
CREATE INDEX IF NOT EXISTS schedules_enabled_next_run_idx
    ON pipeline.schedules (enabled, next_run_at);

-- +statement
ALTER TABLE pipeline.pipelines ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.pipelines FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS pipelines_tenant_isolation ON pipeline.pipelines;

-- +statement
CREATE POLICY pipelines_tenant_isolation ON pipeline.pipelines
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS pipelines_platform_admin ON pipeline.pipelines;

-- +statement
CREATE POLICY pipelines_platform_admin ON pipeline.pipelines
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

-- +statement
ALTER TABLE pipeline.versions ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.versions FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS versions_tenant_isolation ON pipeline.versions;

-- +statement
CREATE POLICY versions_tenant_isolation ON pipeline.versions
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS versions_platform_admin ON pipeline.versions;

-- +statement
CREATE POLICY versions_platform_admin ON pipeline.versions
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

-- +statement
ALTER TABLE pipeline.runs ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.runs FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS runs_tenant_isolation ON pipeline.runs;

-- +statement
CREATE POLICY runs_tenant_isolation ON pipeline.runs
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS runs_platform_admin ON pipeline.runs;

-- +statement
CREATE POLICY runs_platform_admin ON pipeline.runs
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

-- +statement
ALTER TABLE pipeline.schedules ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.schedules FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS schedules_tenant_isolation ON pipeline.schedules;

-- +statement
CREATE POLICY schedules_tenant_isolation ON pipeline.schedules
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS schedules_platform_admin ON pipeline.schedules;

-- +statement
CREATE POLICY schedules_platform_admin ON pipeline.schedules
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');
