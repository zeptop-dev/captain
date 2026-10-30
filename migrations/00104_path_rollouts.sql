-- +goose Up
-- Path intent supplements the existing durable forwarding scheduler. No State,
-- credentials or agent secrets are retained in structural history.
CREATE TABLE path_rollout_intents (
 plan_id TEXT PRIMARY KEY REFERENCES line_rollouts(id) ON DELETE CASCADE,
 before_json TEXT NOT NULL,
 after_json TEXT NOT NULL
);
CREATE TABLE path_rollout_refs (
 plan_id TEXT NOT NULL REFERENCES line_rollouts(id) ON DELETE CASCADE,
 path_id INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 PRIMARY KEY(plan_id,path_id,revision),
 FOREIGN KEY(path_id,revision) REFERENCES forward_path_revisions(path_id,revision) ON DELETE CASCADE
);
-- +goose Down
-- Forward-only: retain active ownership and durable path snapshots.
