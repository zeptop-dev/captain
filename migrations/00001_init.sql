-- +goose Up
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',      -- 'admin' | 'user'
    uuid          TEXT NOT NULL UNIQUE,              -- proxy identity on every inbound
    sub_token     TEXT NOT NULL UNIQUE,              -- subscription URL token
    group_id      INTEGER,                           -- user group -> inbound access
    balance_cents INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'active',    -- 'active' | 'banned'
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE user_groups (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE plans (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL,
    price_cents      INTEGER NOT NULL,
    period_days      INTEGER NOT NULL,                 -- 0 = never expires
    quota_bytes      INTEGER NOT NULL,                 -- 0 = unlimited
    device_limit     INTEGER NOT NULL DEFAULT 0,
    speed_limit_mbps INTEGER NOT NULL DEFAULT 0,
    group_id         INTEGER REFERENCES user_groups(id),
    sort             INTEGER NOT NULL DEFAULT 0,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

-- One active subscription per user; history kept as rows with status 'expired'.
CREATE TABLE subscriptions (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id         INTEGER NOT NULL REFERENCES plans(id),
    starts_at       INTEGER NOT NULL,
    expires_at      INTEGER,                           -- NULL = never
    quota_bytes     INTEGER NOT NULL,
    used_up_bytes   INTEGER NOT NULL DEFAULT 0,
    used_down_bytes INTEGER NOT NULL DEFAULT 0,
    reset_at        INTEGER,                           -- next quota reset
    status          TEXT NOT NULL DEFAULT 'active',    -- 'active' | 'expired'
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX subscriptions_user_status ON subscriptions(user_id, status);

CREATE TABLE orders (
    id           INTEGER PRIMARY KEY,
    no           TEXT NOT NULL UNIQUE,                 -- public order number
    user_id      INTEGER NOT NULL REFERENCES users(id),
    plan_id      INTEGER NOT NULL REFERENCES plans(id),
    amount_cents INTEGER NOT NULL,
    gateway      TEXT NOT NULL,                        -- 'manual' | 'balance' | 'epay' | 'stripe'
    gateway_ref  TEXT,                                 -- gateway's transaction id
    status       TEXT NOT NULL DEFAULT 'pending',      -- 'pending' | 'paid' | 'cancelled'
    created_at   INTEGER NOT NULL,
    paid_at      INTEGER
);
CREATE INDEX orders_user ON orders(user_id);

CREATE TABLE nodes (
    id                   INTEGER PRIMARY KEY,
    name                 TEXT NOT NULL,
    token_hash           TEXT UNIQUE,                  -- sha256 of the agent token, set after pairing
    pair_code            TEXT UNIQUE,
    pair_code_expires_at INTEGER,
    public_addr          TEXT NOT NULL DEFAULT '',
    internal_addr        TEXT NOT NULL DEFAULT '',
    v6_addr              TEXT NOT NULL DEFAULT '',
    monitor_url          TEXT NOT NULL DEFAULT '',     -- external monitoring link (komari, nezha)
    version              TEXT NOT NULL DEFAULT '',
    platform             TEXT NOT NULL DEFAULT '',
    hostname             TEXT NOT NULL DEFAULT '',
    last_seen_at         INTEGER,
    applied_revision     TEXT NOT NULL DEFAULT '',
    host_status_json     TEXT NOT NULL DEFAULT '{}',
    cores_json           TEXT NOT NULL DEFAULT '{}',
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE TABLE inbounds (
    id            INTEGER PRIMARY KEY,
    node_id       INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tag           TEXT NOT NULL,
    protocol      TEXT NOT NULL,
    listen        TEXT NOT NULL DEFAULT '',
    port          INTEGER NOT NULL,
    core          TEXT NOT NULL DEFAULT '',            -- preferred core, '' = auto
    settings_json TEXT NOT NULL DEFAULT '{}',          -- spec.Inbound minus tag/protocol/listen/port/core
    group_id      INTEGER REFERENCES user_groups(id),  -- which user group may use it; NULL = all
    enabled       INTEGER NOT NULL DEFAULT 1,
    sort          INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE(node_id, tag)
);

-- Entries are what subscriptions show. Several may point at one inbound.
CREATE TABLE entries (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    inbound_id   INTEGER NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    chain_id     INTEGER,                              -- relay path in front of the inbound, NULL = direct
    display_host TEXT NOT NULL,
    display_port INTEGER NOT NULL,
    rate         REAL NOT NULL DEFAULT 1.0,            -- traffic multiplier shown to users
    sort         INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE chains (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    hops_json  TEXT NOT NULL,                          -- ordered hops: node_id, port, protocol, connect kind
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE traffic_daily (
    user_id    INTEGER NOT NULL,
    inbound_id INTEGER NOT NULL,
    day        INTEGER NOT NULL,                       -- unix day
    up_bytes   INTEGER NOT NULL DEFAULT 0,
    down_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, inbound_id, day)
);

CREATE TABLE online_devices (
    user_id      INTEGER NOT NULL,
    node_id      INTEGER NOT NULL,
    ip           TEXT NOT NULL,
    last_seen_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, node_id, ip)
);

CREATE TABLE forward_status (
    node_id     INTEGER NOT NULL,
    tag         TEXT NOT NULL,
    up          INTEGER NOT NULL,
    rtt_ms      INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT NOT NULL DEFAULT '',
    active_conn INTEGER NOT NULL DEFAULT 0,
    total_conn  INTEGER NOT NULL DEFAULT 0,
    bytes_in    INTEGER NOT NULL DEFAULT 0,
    bytes_out   INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (node_id, tag)
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
);

-- +goose Down
DROP TABLE settings;
DROP TABLE forward_status;
DROP TABLE online_devices;
DROP TABLE traffic_daily;
DROP TABLE chains;
DROP TABLE entries;
DROP TABLE inbounds;
DROP TABLE nodes;
DROP TABLE orders;
DROP TABLE subscriptions;
DROP TABLE plans;
DROP TABLE user_groups;
DROP TABLE sessions;
DROP TABLE users;
