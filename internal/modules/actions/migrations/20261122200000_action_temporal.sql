-- +goose Up
-- +goose StatementBegin
ALTER TABLE action.action_executions
    ADD COLUMN IF NOT EXISTS object_id uuid NULL,
    ADD COLUMN IF NOT EXISTS definition_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS workflow_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS workflow_run_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attempt int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS current_step_index int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS approval_state text NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS failure_code text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS failure_message text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cancel_requested_at timestamptz NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS action.execution_steps (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    execution_id uuid NOT NULL REFERENCES action.action_executions(id) ON DELETE CASCADE,
    step_index int NOT NULL,
    activity_type text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt int NOT NULL DEFAULT 0,
    input_hash text NOT NULL DEFAULT '',
    before_snapshot jsonb NULL,
    after_snapshot jsonb NULL,
    error text NOT NULL DEFAULT '',
    started_at timestamptz NULL,
    finished_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT execution_steps_execution_step_key UNIQUE (execution_id, step_index)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS execution_steps_tenant_execution_idx
    ON action.execution_steps (tenant_id, execution_id, step_index);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS execution_steps_tenant_status_idx
    ON action.execution_steps (tenant_id, status);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS action.approval_tasks (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    execution_id uuid NOT NULL REFERENCES action.action_executions(id) ON DELETE CASCADE,
    step_index int NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    requested_at timestamptz NOT NULL DEFAULT now(),
    deadline_at timestamptz NOT NULL,
    decided_at timestamptz NULL,
    decided_by uuid NULL,
    comment text NOT NULL DEFAULT '',
    reason_code text NOT NULL DEFAULT '',
    CONSTRAINT approval_tasks_execution_step_key UNIQUE (execution_id, step_index)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS approval_tasks_tenant_status_requested_idx
    ON action.approval_tasks (tenant_id, status, requested_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS approval_tasks_tenant_execution_idx
    ON action.approval_tasks (tenant_id, execution_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS approval_tasks_pending_deadline_idx
    ON action.approval_tasks (deadline_at) WHERE status = 'pending';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS action.idempotency_keys (
    tenant_id uuid NOT NULL,
    key text NOT NULL,
    request_hash text NOT NULL,
    execution_id uuid NOT NULL,
    response jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idempotency_keys_created_at_idx
    ON action.idempotency_keys (created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS action.idempotency_keys;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS action.approval_tasks;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS action.execution_steps;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE action.action_executions
    DROP COLUMN IF EXISTS object_id,
    DROP COLUMN IF EXISTS definition_snapshot,
    DROP COLUMN IF EXISTS workflow_id,
    DROP COLUMN IF EXISTS workflow_run_id,
    DROP COLUMN IF EXISTS attempt,
    DROP COLUMN IF EXISTS current_step_index,
    DROP COLUMN IF EXISTS approval_state,
    DROP COLUMN IF EXISTS failure_code,
    DROP COLUMN IF EXISTS failure_message,
    DROP COLUMN IF EXISTS cancel_requested_at;
-- +goose StatementEnd