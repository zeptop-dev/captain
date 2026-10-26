-- +goose Up
-- Carrier names remain display labels. The opaque measurement definition and
-- address family distinguish protocols, targets and dual-stack observations.
-- Keep the original tables and keys intact so older Captain binaries can still
-- write legacy/custom probes. New history reads include both namespaces without
-- inferring the target or address family of old observations.
CREATE TABLE node_ping_series_stats (
    node_id      INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    task_id      INTEGER NOT NULL,
    name         TEXT NOT NULL,
    series       TEXT NOT NULL DEFAULT '',
    family       TEXT NOT NULL DEFAULT '',
    res          TEXT NOT NULL,
    ts           INTEGER NOT NULL,
    samples      INTEGER NOT NULL DEFAULT 0,
    lost         INTEGER NOT NULL DEFAULT 0,
    sum_ms       REAL NOT NULL DEFAULT 0,
    sum_mbps     REAL NOT NULL DEFAULT 0,
    quality_json TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (node_id, task_id, name, series, family, res, ts)
);
CREATE TABLE node_ping_series_cursors (
    node_id  INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    task_id  INTEGER NOT NULL,
    name     TEXT NOT NULL,
    series   TEXT NOT NULL DEFAULT '',
    family   TEXT NOT NULL DEFAULT '',
    epoch    TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    last_ms  REAL NOT NULL,
    seen_at  INTEGER NOT NULL,
    PRIMARY KEY (node_id, task_id, name, series, family, epoch)
);
CREATE INDEX node_ping_series_cursors_retention ON node_ping_series_cursors(seen_at);
