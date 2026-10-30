-- +goose Up
-- Operational provenance is independent of the lifetime accounting cursor.
CREATE TABLE node_traffic_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    epoch TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    up_bytes INTEGER NOT NULL,
    down_bytes INTEGER NOT NULL,
    totals_known INTEGER NOT NULL,
    discarded_samples INTEGER NOT NULL,
    metadata_status TEXT NOT NULL,
    sample_json TEXT NOT NULL
);
CREATE INDEX node_traffic_history_page ON node_traffic_history(node_id, id DESC);
CREATE INDEX node_traffic_history_retention ON node_traffic_history(received_at);

CREATE TABLE node_traffic_delivery (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    sampled_at TEXT NOT NULL,
    received_at INTEGER NOT NULL,
    delivery_json TEXT NOT NULL
);
