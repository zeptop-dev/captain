-- +goose Up
ALTER TABLE ping_tasks ADD COLUMN source_interface TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Forward-only: preserve measurement path identities.
