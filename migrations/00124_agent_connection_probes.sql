-- +goose Up
ALTER TABLE agent_node_connections ADD COLUMN probe_capable INTEGER NOT NULL DEFAULT 0;
CREATE TABLE agent_connection_probes (
 id TEXT PRIMARY KEY,batch_id TEXT NOT NULL,node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL,source_url TEXT NOT NULL,target_url TEXT NOT NULL,choice_json TEXT NOT NULL,proof_key TEXT NOT NULL,
 status TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,expires_at INTEGER NOT NULL
);
CREATE INDEX agent_probe_node ON agent_connection_probes(node_id,status,created_at);
CREATE INDEX agent_probe_batch ON agent_connection_probes(batch_id,node_id);
-- +goose Down
SELECT 1;
