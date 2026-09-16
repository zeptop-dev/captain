-- +goose Up
-- When a user's traffic was first seen (user.first_connected / user.not_connected events).
ALTER TABLE users ADD COLUMN first_connected_at INTEGER;

-- +goose Down
ALTER TABLE users DROP COLUMN first_connected_at;
