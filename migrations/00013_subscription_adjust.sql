-- +goose Up
ALTER TABLE subscriptions ADD COLUMN quota_override INTEGER NOT NULL DEFAULT 0; -- admin-set quota kept across renewals/resets; 0 = plan default
ALTER TABLE subscriptions ADD COLUMN reset_day INTEGER NOT NULL DEFAULT 0;      -- per-user monthly reset day; 0 = plan mode

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN reset_day;
ALTER TABLE subscriptions DROP COLUMN quota_override;
