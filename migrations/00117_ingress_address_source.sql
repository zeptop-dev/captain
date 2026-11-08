-- +goose Up
ALTER TABLE ingresses ADD COLUMN entry_address_source TEXT NOT NULL DEFAULT 'custom' CHECK(entry_address_source IN ('custom','server_ipv4','server_ipv6'));
ALTER TABLE ingresses ADD COLUMN address_dns_pending INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ingresses ADD COLUMN address_dns_attempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE entries ADD COLUMN follow_ingress INTEGER NOT NULL DEFAULT 0 CHECK(follow_ingress IN (0,1));

-- +goose Down
-- Forward-only migration.
