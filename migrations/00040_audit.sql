-- +goose Up
-- Audit rules (panel-wide, pushed to every node) and the hits nodes report.
CREATE TABLE audit_rules (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    match_json TEXT NOT NULL,                 -- JSON array of match entries (route-rule syntax)
    action     TEXT NOT NULL DEFAULT 'block', -- block | log
    enabled    INTEGER NOT NULL DEFAULT 1,
    sort       INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL,
    node_id    INTEGER NOT NULL,
    inbound_id INTEGER NOT NULL DEFAULT 0,
    rule_id    INTEGER NOT NULL,
    at         INTEGER NOT NULL,
    client_ip  TEXT NOT NULL DEFAULT '',
    host       TEXT NOT NULL,
    port       INTEGER NOT NULL DEFAULT 0,
    action     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_user_at ON audit_log (user_id, at);
CREATE INDEX audit_log_at ON audit_log (at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE audit_rules;
