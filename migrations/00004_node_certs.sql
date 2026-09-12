-- +goose Up
ALTER TABLE nodes ADD COLUMN certs_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE nodes DROP COLUMN certs_json;
