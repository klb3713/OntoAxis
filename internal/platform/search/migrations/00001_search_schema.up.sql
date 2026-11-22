-- +goose Up
CREATE SCHEMA IF NOT EXISTS search;

CREATE TABLE IF NOT EXISTS search.outbox_attempt (
    event_id uuid PRIMARY KEY,
    attempts int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    last_error text,
    dead_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS search.index_state (
    tenant_id uuid NOT NULL,
    index_version text NOT NULL,
    index_uid text NOT NULL,
    last_event_at timestamptz,
    last_success_at timestamptz,
    rebuilt_at timestamptz,
    PRIMARY KEY (tenant_id, index_version)
);

-- +goose Down
DROP TABLE IF EXISTS search.index_state;
DROP TABLE IF EXISTS search.outbox_attempt;
DROP SCHEMA IF EXISTS search;
