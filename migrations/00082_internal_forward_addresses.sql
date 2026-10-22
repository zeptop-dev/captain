-- +goose Up
ALTER TABLE forward_lines ADD COLUMN target_address_mode TEXT NOT NULL DEFAULT '';

-- +goose Down
SELECT 1;
