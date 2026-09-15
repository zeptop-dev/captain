-- +goose Up
ALTER TABLE nodes ADD COLUMN overrides_json TEXT NOT NULL DEFAULT '{}';   -- map[core]JSON object merged into the rendered config

-- +goose Down
ALTER TABLE nodes DROP COLUMN overrides_json;
