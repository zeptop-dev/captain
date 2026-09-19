-- +goose Up
-- The order that bought a queued subscription, so cancelling the row
-- refunds that order and no other. NULL for admin grants, gift codes,
-- trials and rows from before this column (no automatic refund for those).
ALTER TABLE subscriptions ADD COLUMN order_id INTEGER;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN order_id;
