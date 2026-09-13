-- +goose Up
ALTER TABLE nodes ADD COLUMN forwards_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE nodes DROP COLUMN forwards_json;
