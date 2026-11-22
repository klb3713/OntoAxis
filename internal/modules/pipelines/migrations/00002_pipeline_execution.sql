-- +statement
ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS execution_id text;

-- +statement
ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS failure_code text;

-- +statement
ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS attempt_count integer NOT NULL DEFAULT 0;

-- +statement
ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS last_reconciled_at timestamptz;

-- +statement
CREATE INDEX IF NOT EXISTS runs_tenant_status_started_idx
    ON pipeline.runs (tenant_id, status, started_at);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.run_inputs (
    run_id uuid NOT NULL REFERENCES pipeline.runs (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, dataset_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.run_outputs (
    run_id uuid NOT NULL REFERENCES pipeline.runs (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL,
    model text NOT NULL,
    relation_catalog text NOT NULL,
    relation_schema text NOT NULL,
    relation_table text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, dataset_id)
);

-- +statement
CREATE INDEX IF NOT EXISTS run_outputs_run_idx
    ON pipeline.run_outputs (run_id);

-- +statement
CREATE TABLE IF NOT EXISTS pipeline.idempotency_keys (
    tenant_id uuid NOT NULL,
    operation text NOT NULL,
    key_hash text NOT NULL,
    request_hash text NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, operation, key_hash)
);

-- +statement
CREATE INDEX IF NOT EXISTS idempotency_keys_expires_idx
    ON pipeline.idempotency_keys (expires_at);

-- +statement
ALTER TABLE pipeline.run_inputs ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.run_inputs FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS run_inputs_tenant_isolation ON pipeline.run_inputs;

-- +statement
CREATE POLICY run_inputs_tenant_isolation ON pipeline.run_inputs
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS run_inputs_platform_admin ON pipeline.run_inputs;

-- +statement
CREATE POLICY run_inputs_platform_admin ON pipeline.run_inputs
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

-- +statement
ALTER TABLE pipeline.run_outputs ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.run_outputs FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS run_outputs_tenant_isolation ON pipeline.run_outputs;

-- +statement
CREATE POLICY run_outputs_tenant_isolation ON pipeline.run_outputs
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS run_outputs_platform_admin ON pipeline.run_outputs;

-- +statement
CREATE POLICY run_outputs_platform_admin ON pipeline.run_outputs
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');

-- +statement
ALTER TABLE pipeline.idempotency_keys ENABLE ROW LEVEL SECURITY;

-- +statement
ALTER TABLE pipeline.idempotency_keys FORCE ROW LEVEL SECURITY;

-- +statement
DROP POLICY IF EXISTS idempotency_keys_tenant_isolation ON pipeline.idempotency_keys;

-- +statement
CREATE POLICY idempotency_keys_tenant_isolation ON pipeline.idempotency_keys
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- +statement
DROP POLICY IF EXISTS idempotency_keys_platform_admin ON pipeline.idempotency_keys;

-- +statement
CREATE POLICY idempotency_keys_platform_admin ON pipeline.idempotency_keys
    FOR ALL
    USING (current_setting('app.is_platform_admin', true) = 'true')
    WITH CHECK (current_setting('app.is_platform_admin', true) = 'true');