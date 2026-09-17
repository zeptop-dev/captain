-- +goose Up
-- Which column a commission was paid into, so cancelling the order can
-- reverse it even after the invite payout setting changed.
ALTER TABLE commissions ADD COLUMN payout TEXT NOT NULL DEFAULT 'balance';

-- +goose Down
ALTER TABLE commissions DROP COLUMN payout;
