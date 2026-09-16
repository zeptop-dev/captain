-- +goose Up
-- Connection log: one row per accepted connection a node reported
-- (Settings → Connection log; off by default, pruned by retention).
CREATE TABLE conn_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL,
    node_id    INTEGER NOT NULL,
    inbound_id INTEGER NOT NULL DEFAULT 0,
    at         INTEGER NOT NULL,
    client_ip  TEXT NOT NULL DEFAULT '',
    host       TEXT NOT NULL,
    port       INTEGER NOT NULL DEFAULT 0,
    network    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX conn_log_user_at ON conn_log (user_id, at);
CREATE INDEX conn_log_at ON conn_log (at);

-- +goose Down
DROP TABLE conn_log;
