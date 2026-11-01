-- +goose Up
-- Bounded, typed evidence only. Parent delivery pruning/delete/reset removes it.
CREATE TABLE node_rendered_configs (
    delivery_id TEXT PRIMARY KEY REFERENCES node_config_deliveries(id) ON DELETE CASCADE,
    receipt_sequence INTEGER NOT NULL,
    metadata_status TEXT NOT NULL CHECK(metadata_status IN ('valid','invalid','unavailable')),
    evidence_json TEXT NOT NULL DEFAULT '' CHECK(length(evidence_json) <= 65536)
);

-- +goose Down
-- Forward-only: old binaries ignore this additive observation table.
