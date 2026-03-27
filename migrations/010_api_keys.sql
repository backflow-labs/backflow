-- +goose Up

CREATE TABLE api_keys (
    key_hash    TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    permissions JSONB NOT NULL DEFAULT '[]',
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_expires_at ON api_keys(expires_at);

-- +goose Down

DROP TABLE IF EXISTS api_keys;
