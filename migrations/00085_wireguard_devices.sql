-- +goose Up
-- Never reuse device IDs: their addresses are credentials already installed on clients.
CREATE TABLE wireguard_devices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    uuid TEXT NOT NULL UNIQUE,
    token TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER
);
CREATE INDEX wireguard_devices_user ON wireguard_devices(user_id);

-- +goose Down
DROP TABLE wireguard_devices;
