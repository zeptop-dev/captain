-- +goose Up
ALTER TABLE nodes ADD COLUMN doctor_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN doctor_json;
