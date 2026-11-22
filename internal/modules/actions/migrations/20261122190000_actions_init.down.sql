-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS action.action_executions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS action.action_types;
-- +goose StatementEnd
