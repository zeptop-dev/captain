-- +goose Up
-- Several subscriptions may be active at once; 'queued' ones start when the
-- user's current plans lapse. period_days is remembered for that start.
ALTER TABLE subscriptions ADD COLUMN period_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN activation TEXT NOT NULL DEFAULT '';   -- '' = start now (stack/renew), 'queue' = after current plans lapse

-- +goose Down
ALTER TABLE orders DROP COLUMN activation;
ALTER TABLE subscriptions DROP COLUMN period_days;
