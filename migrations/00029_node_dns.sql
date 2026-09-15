-- +goose Up
ALTER TABLE nodes ADD COLUMN dns_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE nodes DROP COLUMN dns_json;
