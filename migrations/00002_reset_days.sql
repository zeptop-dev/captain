-- +goose Up
ALTER TABLE plans ADD COLUMN reset_days INTEGER NOT NULL DEFAULT 0;  -- 0 = quota never resets within the period

-- +goose Down
ALTER TABLE plans DROP COLUMN reset_days;
