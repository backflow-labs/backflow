-- +goose Up
CREATE TABLE discord_installs (
    guild_id       TEXT PRIMARY KEY,
    app_id         TEXT NOT NULL,
    channel_id     TEXT NOT NULL,
    allowed_roles  JSONB NOT NULL DEFAULT '[]',
    installed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS discord_installs;
