-- +goose Up
-- Carrier names remain display labels. The opaque measurement definition and
-- address family distinguish protocols, targets and dual-stack observations.
-- Keep old rows in the empty series/family namespace without inferring what
-- their historical target or address family was.
CREATE TABLE node_ping_stats_series (
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
INSERT INTO node_ping_stats_series (node_id, task_id, name, res, ts, samples, lost, sum_ms, sum_mbps, quality_json)
    SELECT node_id, task_id, name, res, ts, samples, lost, sum_ms, sum_mbps, quality_json FROM node_ping_stats;
DROP TABLE node_ping_stats;
ALTER TABLE node_ping_stats_series RENAME TO node_ping_stats;

CREATE TABLE node_ping_cursors_series (
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
INSERT INTO node_ping_cursors_series (node_id, task_id, name, epoch, sequence, last_ms, seen_at)
    SELECT node_id, task_id, name, epoch, sequence, last_ms, seen_at FROM node_ping_cursors;
DROP TABLE node_ping_cursors;
ALTER TABLE node_ping_cursors_series RENAME TO node_ping_cursors;
CREATE INDEX node_ping_cursors_retention ON node_ping_cursors(seen_at);
