-- +goose Up
ALTER TABLE nodes ADD COLUMN decoy_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN decoy_upstream TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN decoy_upstream;
ALTER TABLE nodes DROP COLUMN decoy_enabled;
