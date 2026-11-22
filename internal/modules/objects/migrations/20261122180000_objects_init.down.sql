-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS object_data.object_links;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS object_data.objects;
-- +goose StatementEnd
