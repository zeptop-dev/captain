-- +goose Up
-- Structural, line-owned forwarding only. No complete State, users or secrets.
CREATE TABLE line_rollouts (
 ordinal INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 line_id INTEGER NOT NULL,
 scope_id TEXT NOT NULL REFERENCES line_change_scopes(id) ON DELETE CASCADE,
 version INTEGER NOT NULL DEFAULT 1,
 snapshot_version TEXT NOT NULL DEFAULT 'line-relay-v1',
 checked_at INTEGER NOT NULL DEFAULT 0,
 kind TEXT NOT NULL,
 status TEXT NOT NULL,
 stage TEXT NOT NULL,
 actor TEXT NOT NULL,
 before_json TEXT NOT NULL,
 after_json TEXT NOT NULL,
 dependencies_json TEXT NOT NULL,
 base_version INTEGER NOT NULL,
 timeout_ms INTEGER NOT NULL,
 error_code TEXT NOT NULL DEFAULT '',
 compensates TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 published_at INTEGER
);
CREATE UNIQUE INDEX line_rollouts_active ON line_rollouts(scope_id) WHERE status IN ('running','paused');
CREATE INDEX line_rollouts_history ON line_rollouts(line_id,ordinal DESC);
CREATE TABLE line_rollout_nodes (
 plan_id TEXT NOT NULL REFERENCES line_rollouts(id) ON DELETE CASCADE,
 node_id INTEGER NOT NULL,
 node_name TEXT NOT NULL,
 node_identity TEXT NOT NULL,
 token_identity TEXT NOT NULL,
 before_json TEXT NOT NULL,
 after_json TEXT NOT NULL,
 before_depth INTEGER NOT NULL,
 after_depth INTEGER NOT NULL,
 PRIMARY KEY(plan_id,node_id)
);
CREATE TABLE line_rollout_steps (
 id TEXT PRIMARY KEY,
 plan_id TEXT NOT NULL REFERENCES line_rollouts(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL,
 node_id INTEGER NOT NULL,
 phase TEXT NOT NULL,
 expected_json TEXT NOT NULL,
 status TEXT NOT NULL,
 attempt INTEGER NOT NULL DEFAULT 1,
 attempt_id TEXT NOT NULL,
 deadline_at INTEGER,
 delivery_id TEXT NOT NULL DEFAULT '',
 session TEXT NOT NULL DEFAULT '',
 config_revision TEXT NOT NULL DEFAULT '',
 state_revision TEXT NOT NULL DEFAULT '',
 issued_at INTEGER,
 confirmed_at INTEGER,
 observed_at INTEGER,
 last_receipt_at INTEGER,
 receipt_sequence INTEGER NOT NULL DEFAULT 0,
 receipt_applied INTEGER NOT NULL DEFAULT 0,
 UNIQUE(plan_id,ordinal)
);
CREATE INDEX line_rollout_steps_node ON line_rollout_steps(node_id,plan_id);
-- Candidate associations are bounded by the existing 100-delivery window.
CREATE TABLE line_rollout_deliveries (
 delivery_id TEXT NOT NULL REFERENCES node_config_deliveries(id) ON DELETE CASCADE,
 step_id TEXT NOT NULL REFERENCES line_rollout_steps(id) ON DELETE CASCADE,
 attempt_id TEXT NOT NULL,
 PRIMARY KEY(delivery_id,step_id,attempt_id)
);
-- Last receipt per attempt, independently retained across delivery pruning.
CREATE TABLE line_rollout_attempts (
 step_id TEXT NOT NULL REFERENCES line_rollout_steps(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL,
 delivery_id TEXT NOT NULL,
 config_revision TEXT NOT NULL,
 state_revision TEXT NOT NULL,
 observed_at INTEGER NOT NULL,
 received_at INTEGER NOT NULL,
 applied INTEGER NOT NULL,
 PRIMARY KEY(step_id,attempt)
);
-- Safe structural observations used only to establish a re-paired identity's
-- own compensation baseline. Bounded with delivery retention, no whole State.
CREATE TABLE line_rollout_observations (
 delivery_id TEXT NOT NULL REFERENCES node_config_deliveries(id) ON DELETE CASCADE,
 plan_id TEXT NOT NULL REFERENCES line_rollouts(id) ON DELETE CASCADE,
 node_id INTEGER NOT NULL,
 node_identity TEXT NOT NULL,
 token_identity TEXT NOT NULL,
 forwards_json TEXT NOT NULL,
 PRIMARY KEY(delivery_id,plan_id,node_id)
);
-- +goose Down
-- Forward only: retain immutable plans and current forwarding pointers.
