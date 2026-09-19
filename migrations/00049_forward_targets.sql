-- +goose Up
-- Per-hop health of a forward with several targets (bosun >= 0.49 reports
-- it): JSON array of {target, up, rtt_ms, last_error, active_conn,
-- total_conn}. Empty for single-target rules and older agents.
ALTER TABLE forward_status ADD COLUMN targets_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE forward_status DROP COLUMN targets_json;
