-- +goose Up
-- HWID device limit: clients that send an x-hwid header (Happ, FlClashX,
-- Koala Clash, V2Box, Streisand …) are counted per device, not per IP.
CREATE TABLE hwid_devices (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hwid          TEXT    NOT NULL,
    platform      TEXT    NOT NULL DEFAULT '',
    os_version    TEXT    NOT NULL DEFAULT '',
    device_model  TEXT    NOT NULL DEFAULT '',
    user_agent    TEXT    NOT NULL DEFAULT '',
    request_ip    TEXT    NOT NULL DEFAULT '',
    first_seen_at INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, hwid)
);
-- NULL = the plan's device limit applies; 0 = no HWID limit for this user.
ALTER TABLE users ADD COLUMN hwid_limit INTEGER;

-- Every subscription fetch, for support and abuse checks (pruned after 30 days).
CREATE TABLE sub_requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    at            INTEGER NOT NULL,
    request_ip    TEXT    NOT NULL DEFAULT '',
    user_agent    TEXT    NOT NULL DEFAULT '',
    hwid          TEXT    NOT NULL DEFAULT '',
    rule          TEXT    NOT NULL DEFAULT '',
    response      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sub_requests_user_at ON sub_requests(user_id, at);
CREATE INDEX sub_requests_at ON sub_requests(at);

-- +goose Down
DROP TABLE sub_requests;
ALTER TABLE users DROP COLUMN hwid_limit;
DROP TABLE hwid_devices;
