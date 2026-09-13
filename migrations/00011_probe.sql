-- +goose Up
ALTER TABLE nodes ADD COLUMN probe_hidden INTEGER NOT NULL DEFAULT 0;        -- keep off the public probe page
ALTER TABLE nodes ADD COLUMN probe_info_json TEXT NOT NULL DEFAULT '{}';    -- display facts: region, provider, price, expires
ALTER TABLE nodes ADD COLUMN traffic_limit_bytes INTEGER NOT NULL DEFAULT 0; -- monthly NIC allowance, 0 = none
ALTER TABLE nodes ADD COLUMN traffic_reset_day INTEGER NOT NULL DEFAULT 1;   -- day of month the period starts
ALTER TABLE nodes ADD COLUMN traffic_mode TEXT NOT NULL DEFAULT 'sum';       -- sum | up | down | max
ALTER TABLE nodes ADD COLUMN traffic_period_start INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN traffic_used_up INTEGER NOT NULL DEFAULT 0;     -- bytes this period
ALTER TABLE nodes ADD COLUMN traffic_used_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN traffic_last_up INTEGER NOT NULL DEFAULT 0;     -- last seen NIC counters (reset-aware deltas)
ALTER TABLE nodes ADD COLUMN traffic_last_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN traffic_prev_used INTEGER NOT NULL DEFAULT 0;   -- last period's billed bytes

CREATE TABLE node_stats (
    node_id   INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    res       TEXT NOT NULL,      -- 'm' minute, 'h' hour, 'd' day
    ts        INTEGER NOT NULL,   -- bucket start, unix seconds
    samples   INTEGER NOT NULL DEFAULT 0,
    cpu       REAL NOT NULL DEFAULT 0,      -- sums; divide by samples
    mem_used  REAL NOT NULL DEFAULT 0,
    mem_total INTEGER NOT NULL DEFAULT 0,   -- last value
    swap_used REAL NOT NULL DEFAULT 0,
    disk_used REAL NOT NULL DEFAULT 0,
    disk_total INTEGER NOT NULL DEFAULT 0,
    net_up    REAL NOT NULL DEFAULT 0,      -- bytes/s sums
    net_down  REAL NOT NULL DEFAULT 0,
    load1     REAL NOT NULL DEFAULT 0,
    tcp       REAL NOT NULL DEFAULT 0,
    udp       REAL NOT NULL DEFAULT 0,
    procs     REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, res, ts)
);
CREATE TABLE node_ping_stats (
    node_id   INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    task_id   INTEGER NOT NULL,   -- 0 = carrier probes (name distinguishes)
    name      TEXT NOT NULL,
    res       TEXT NOT NULL,
    ts        INTEGER NOT NULL,
    samples   INTEGER NOT NULL DEFAULT 0,
    lost      INTEGER NOT NULL DEFAULT 0,
    sum_ms    REAL NOT NULL DEFAULT 0,   -- over successful samples
    PRIMARY KEY (node_id, task_id, name, res, ts)
);
CREATE TABLE ping_tasks (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL,
    type             TEXT NOT NULL DEFAULT 'tcp',   -- icmp | tcp | http
    target           TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL DEFAULT 30,
    node_ids_json    TEXT NOT NULL DEFAULT '[]',    -- empty = every node
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL
);
CREATE TABLE probe_alerts (
    node_id  INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,       -- offline | cpu | mem | disk | traffic80 | traffic100
    fired_at INTEGER NOT NULL,
    PRIMARY KEY (node_id, kind)
);

-- +goose Down
DROP TABLE probe_alerts;
DROP TABLE ping_tasks;
DROP TABLE node_ping_stats;
DROP TABLE node_stats;
ALTER TABLE nodes DROP COLUMN traffic_prev_used;
ALTER TABLE nodes DROP COLUMN traffic_last_down;
ALTER TABLE nodes DROP COLUMN traffic_last_up;
ALTER TABLE nodes DROP COLUMN traffic_used_down;
ALTER TABLE nodes DROP COLUMN traffic_used_up;
ALTER TABLE nodes DROP COLUMN traffic_period_start;
ALTER TABLE nodes DROP COLUMN traffic_mode;
ALTER TABLE nodes DROP COLUMN traffic_reset_day;
ALTER TABLE nodes DROP COLUMN traffic_limit_bytes;
ALTER TABLE nodes DROP COLUMN probe_info_json;
ALTER TABLE nodes DROP COLUMN probe_hidden;
