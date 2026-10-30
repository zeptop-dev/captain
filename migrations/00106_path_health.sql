-- +goose Up
-- Capability evidence and samples are bound to the current authenticated
-- process. A report retry cannot refresh either observation clock.
CREATE TABLE path_health_capabilities (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    token_identity TEXT NOT NULL,
    session TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    observed_at INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    supported INTEGER NOT NULL,
    epoch TEXT NOT NULL DEFAULT ''
);
CREATE TABLE path_health_deliveries (
    delivery_id TEXT NOT NULL REFERENCES node_config_deliveries(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    health_json TEXT NOT NULL,
    PRIMARY KEY(delivery_id,tag)
);
CREATE TABLE path_health_samples (
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    token_identity TEXT NOT NULL,
    session TEXT NOT NULL,
    epoch TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    observed_at INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    sample_json TEXT NOT NULL,
    PRIMARY KEY(node_id,tag,candidate_id)
);

-- +goose Down
SELECT 1;
