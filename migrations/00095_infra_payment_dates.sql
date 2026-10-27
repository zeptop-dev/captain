-- +goose Up
-- Existing records retain the period-advance meaning. New record-only payments
-- store empty period dates rather than claiming an already-extended paid term.
ALTER TABLE infra_payments ADD COLUMN date_action TEXT NOT NULL DEFAULT 'advance_period';

-- +goose Down
-- Forward-only: retain the meaning of historical payment date operations.
