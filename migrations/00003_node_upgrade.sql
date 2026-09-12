-- +goose Up
ALTER TABLE nodes ADD COLUMN upgrade_to TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN upgrade_to;
