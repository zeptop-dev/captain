-- +goose Up
ALTER TABLE nodes ADD COLUMN warp_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN warp_json;
