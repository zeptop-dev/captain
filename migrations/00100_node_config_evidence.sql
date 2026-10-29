-- +goose Up
-- Only delivery identities and digests are retained, never rendered configuration.
CREATE TABLE node_config_sessions (
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    token_identity TEXT NOT NULL,
    session TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    retired_at INTEGER,
    receipt_sequence INTEGER NOT NULL DEFAULT 0,
    observed_at INTEGER,
    last_receipt_at INTEGER,
    PRIMARY KEY(node_id, token_identity, session)
);
CREATE TABLE node_config_deliveries (
    ordinal INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    token_identity TEXT NOT NULL,
    session TEXT NOT NULL,
    config_revision TEXT NOT NULL,
    state_revision TEXT NOT NULL,
    issued_at INTEGER NOT NULL,
    confirmed_at INTEGER,
    last_receipt_at INTEGER,
    observed_at INTEGER,
    receipt_sequence INTEGER NOT NULL DEFAULT 0,
    applied INTEGER NOT NULL DEFAULT 0 CHECK(applied IN (0,1)),
    FOREIGN KEY(node_id, token_identity, session) REFERENCES node_config_sessions(node_id, token_identity, session) ON DELETE CASCADE
);
CREATE INDEX node_config_deliveries_history ON node_config_deliveries(node_id, ordinal DESC);
CREATE TABLE node_config_heads (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    token_identity TEXT NOT NULL,
    session TEXT NOT NULL,
    delivery_id TEXT NOT NULL REFERENCES node_config_deliveries(id),
    FOREIGN KEY(node_id, token_identity, session) REFERENCES node_config_sessions(node_id, token_identity, session) ON DELETE CASCADE
);

-- +goose Down
-- Forward-only: preserve delivery history and retired-session fencing.
