-- +goose Up

ALTER TABLE tasks ADD COLUMN force BOOLEAN NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE tasks DROP COLUMN IF EXISTS force;
