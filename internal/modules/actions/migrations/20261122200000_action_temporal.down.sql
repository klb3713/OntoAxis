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