-- +goose Up
ALTER TABLE agent_connection_migrations ADD COLUMN verification_mode TEXT NOT NULL DEFAULT 'direct';
ALTER TABLE agent_node_connections ADD COLUMN proxy_capable INTEGER NOT NULL DEFAULT 0;
-- +goose Down
SELECT 1;
