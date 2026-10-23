-- +goose Up
ALTER TABLE nodes ADD COLUMN probe_storage_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
SELECT 1;
