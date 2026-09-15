-- +goose Up
ALTER TABLE nodes ADD COLUMN user_speed_limit_mbps INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nodes DROP COLUMN user_speed_limit_mbps;
