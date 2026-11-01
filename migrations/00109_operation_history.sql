-- +goose Up
-- Existing operations keep their original identities and retention policies.
-- Only future upgrade requests acquire history; old mutable upgrade slots are
-- not reconstructed as successful or executed operations.
CREATE TABLE node_upgrade_operations (
 id TEXT PRIMARY KEY,
 node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 node_name TEXT NOT NULL,
 token_identity TEXT NOT NULL,
 target_version TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','observed','superseded','cancelled','identity_changed')),
 actor TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 completed_at INTEGER,
 supersedes TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX node_upgrade_operations_active ON node_upgrade_operations(node_id) WHERE status='pending';
CREATE INDEX node_upgrade_operations_history ON node_upgrade_operations(created_at DESC,id DESC);
CREATE INDEX node_upgrade_operations_node ON node_upgrade_operations(node_id,created_at DESC,id DESC);
CREATE INDEX node_jobs_operation_history ON node_jobs(created_at DESC,id DESC);
CREATE INDEX line_rollouts_operation_history ON line_rollouts(created_at DESC,id DESC);
CREATE INDEX topology_changesets_operation_history ON topology_changesets(created_at DESC,id DESC);
-- Pairing revocation immediately retires the old operation, even if the same
-- numeric node ID remains. No token material is included in public projections.
-- +goose StatementBegin
CREATE TRIGGER node_upgrade_identity_changed AFTER UPDATE OF token_hash ON nodes
WHEN COALESCE(OLD.token_hash,'') <> COALESCE(NEW.token_hash,'') BEGIN
 UPDATE node_upgrade_operations SET status='identity_changed',updated_at=CAST(unixepoch('subsec')*1000 AS INTEGER),completed_at=CAST(unixepoch('subsec')*1000 AS INTEGER)
 WHERE node_id=NEW.id AND status='pending';
 DELETE FROM node_upgrade_operations WHERE node_id=NEW.id AND status<>'pending' AND id NOT IN
 (SELECT id FROM node_upgrade_operations WHERE node_id=NEW.id AND status<>'pending' ORDER BY created_at DESC,id DESC LIMIT 200);
 DELETE FROM node_upgrade_operations WHERE status<>'pending' AND id NOT IN
 (SELECT id FROM node_upgrade_operations WHERE status<>'pending' ORDER BY created_at DESC,id DESC LIMIT 10000);
END;
-- +goose StatementEnd
-- +goose Down
-- Forward-only: preserve operation identities and observations.
