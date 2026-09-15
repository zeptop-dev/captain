-- +goose Up
ALTER TABLE sessions ADD COLUMN admin INTEGER NOT NULL DEFAULT 0;   -- 1 = minted by the admin login (password + TOTP); portal/OIDC/reset sessions cannot reach /api/admin
CREATE INDEX IF NOT EXISTS traffic_daily_day ON traffic_daily(day);
CREATE INDEX IF NOT EXISTS subscriptions_status_expires ON subscriptions(status, expires_at);
CREATE INDEX IF NOT EXISTS subscriptions_status_reset ON subscriptions(status, reset_at);
CREATE INDEX IF NOT EXISTS orders_status_created ON orders(status, created_at);

-- +goose Down
DROP INDEX IF EXISTS orders_status_created;
DROP INDEX IF EXISTS subscriptions_status_reset;
DROP INDEX IF EXISTS subscriptions_status_expires;
DROP INDEX IF EXISTS traffic_daily_day;
ALTER TABLE sessions DROP COLUMN admin;
