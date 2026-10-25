-- +goose Up
-- Resource-only heartbeats must not make old proxy runtime observations fresh.
ALTER TABLE nodes ADD COLUMN runtime_reported_at INTEGER;
ALTER TABLE nodes ADD COLUMN runtime_report_seq INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Forward-only: retaining these columns keeps historical observations intact.
