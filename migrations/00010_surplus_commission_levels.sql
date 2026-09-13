-- +goose Up
ALTER TABLE orders ADD COLUMN surplus_cents INTEGER NOT NULL DEFAULT 0;   -- credit for the unused part of the previous plan
ALTER TABLE users ADD COLUMN commission_cents INTEGER NOT NULL DEFAULT 0; -- withdrawable referral earnings (payout mode "commission")
CREATE TABLE commissions_new (
    id           INTEGER PRIMARY KEY,
    order_id     INTEGER NOT NULL REFERENCES orders(id),
    inviter_id   INTEGER NOT NULL REFERENCES users(id),
    invitee_id   INTEGER NOT NULL REFERENCES users(id),
    level        INTEGER NOT NULL DEFAULT 1,
    amount_cents INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE(order_id, level)
);
INSERT INTO commissions_new (id, order_id, inviter_id, invitee_id, level, amount_cents, created_at)
    SELECT id, order_id, inviter_id, invitee_id, 1, amount_cents, created_at FROM commissions;
DROP TABLE commissions;
ALTER TABLE commissions_new RENAME TO commissions;
CREATE INDEX commissions_inviter ON commissions(inviter_id, created_at);
CREATE TABLE withdrawals (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL,
    method       TEXT NOT NULL DEFAULT '',
    account      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending',   -- pending | paid | rejected
    note         TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX withdrawals_status ON withdrawals(status, id);

-- +goose Down
DROP TABLE withdrawals;
DROP INDEX commissions_inviter;
ALTER TABLE users DROP COLUMN commission_cents;
ALTER TABLE orders DROP COLUMN surplus_cents;
