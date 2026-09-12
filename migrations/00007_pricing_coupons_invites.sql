-- +goose Up
ALTER TABLE plans ADD COLUMN prices_json TEXT NOT NULL DEFAULT '[]';   -- extra periods: [{"period_days":90,"price_cents":2700}]
ALTER TABLE plans ADD COLUMN reset_mode TEXT NOT NULL DEFAULT '';      -- '' (never / legacy reset_days), 'days', 'monthly', 'yearly'
ALTER TABLE orders ADD COLUMN period_days INTEGER NOT NULL DEFAULT 0;   -- chosen period; 0 = the plan's base period
ALTER TABLE orders ADD COLUMN coupon_id INTEGER;
ALTER TABLE orders ADD COLUMN discount_cents INTEGER NOT NULL DEFAULT 0;
CREATE TABLE coupons (
    id             INTEGER PRIMARY KEY,
    code           TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL,                     -- 'percent' | 'fixed'
    value          INTEGER NOT NULL,                  -- percent off, or cents off
    plan_ids_json  TEXT NOT NULL DEFAULT '[]',        -- empty = any plan
    max_uses       INTEGER NOT NULL DEFAULT 0,        -- 0 = unlimited
    used           INTEGER NOT NULL DEFAULT 0,
    per_user       INTEGER NOT NULL DEFAULT 1,        -- uses per user; 0 = unlimited
    starts_at      INTEGER,
    expires_at     INTEGER,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL
);
ALTER TABLE users ADD COLUMN invite_code TEXT;
ALTER TABLE users ADD COLUMN invited_by INTEGER REFERENCES users(id);
CREATE UNIQUE INDEX users_invite_code ON users(invite_code) WHERE invite_code IS NOT NULL;
CREATE TABLE commissions (
    id           INTEGER PRIMARY KEY,
    order_id     INTEGER NOT NULL UNIQUE REFERENCES orders(id),
    inviter_id   INTEGER NOT NULL REFERENCES users(id),
    invitee_id   INTEGER NOT NULL REFERENCES users(id),
    amount_cents INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
);

-- +goose Down
DROP TABLE commissions;
DROP INDEX users_invite_code;
ALTER TABLE users DROP COLUMN invited_by;
ALTER TABLE users DROP COLUMN invite_code;
DROP TABLE coupons;
ALTER TABLE orders DROP COLUMN discount_cents;
ALTER TABLE orders DROP COLUMN coupon_id;
ALTER TABLE orders DROP COLUMN period_days;
ALTER TABLE plans DROP COLUMN reset_mode;
ALTER TABLE plans DROP COLUMN prices_json;
