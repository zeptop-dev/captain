-- +goose Up
ALTER TABLE entries ADD COLUMN tags TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN region TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE entries DROP COLUMN region;
ALTER TABLE entries DROP COLUMN tags;
