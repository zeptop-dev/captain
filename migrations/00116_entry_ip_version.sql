-- +goose Up
ALTER TABLE entries ADD COLUMN ip_version TEXT NOT NULL DEFAULT '' CHECK(ip_version IN ('','ipv4','ipv6','dual'));

-- +goose Down
-- Forward-only migration.
