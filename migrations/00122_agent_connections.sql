-- +goose Up
CREATE TABLE agent_endpoints (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, url TEXT NOT NULL, listen TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0, certificate_id INTEGER NOT NULL DEFAULT 0,
 revision TEXT NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, was_default INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX agent_endpoint_url ON agent_endpoints(url) WHERE url<>'';
CREATE UNIQUE INDEX agent_endpoint_default ON agent_endpoints(is_default) WHERE is_default=1;
INSERT INTO agent_endpoints
 SELECT 'legacy','Existing Agent HTTPS',coalesce(json_extract(value_json,'$.url'),''),coalesce(json_extract(value_json,'$.listen'),':2096'),
 coalesce(json_extract(value_json,'$.enabled'),0),coalesce(json_extract(value_json,'$.certificate_id'),0),lower(hex(randomblob(24))),coalesce(json_extract(value_json,'$.enabled'),0),coalesce(json_extract(value_json,'$.enabled'),0)
 FROM settings WHERE key='agent_https' AND (coalesce(json_extract(value_json,'$.url'),'')<>'' OR coalesce(json_extract(value_json,'$.certificate_id'),0)>0);
CREATE TABLE agent_node_connections (
 node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL DEFAULT '', current_url TEXT NOT NULL DEFAULT '', seen_at INTEGER NOT NULL DEFAULT 0,
 capable INTEGER NOT NULL DEFAULT 0, mode TEXT NOT NULL DEFAULT 'default', endpoint_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE agent_connection_migrations (
 id TEXT PRIMARY KEY, batch_id TEXT NOT NULL, node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL, source_url TEXT NOT NULL, target_url TEXT NOT NULL, endpoint_id TEXT NOT NULL DEFAULT '',
 proof_key TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, issued_at INTEGER NOT NULL DEFAULT 0, expires_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX agent_connection_active ON agent_connection_migrations(node_id) WHERE status IN ('queued','issued','verified');
CREATE INDEX agent_connection_queue ON agent_connection_migrations(status,created_at);
-- +goose Down
SELECT 1;
