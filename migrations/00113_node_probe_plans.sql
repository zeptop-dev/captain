-- +goose Up
CREATE TABLE node_probe_plans (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    mode TEXT NOT NULL DEFAULT 'inherit',
    revision TEXT NOT NULL,
    custom_json TEXT NOT NULL DEFAULT 'null',
    changed_at INTEGER NOT NULL DEFAULT 0
);
INSERT INTO node_probe_plans(node_id, revision) SELECT id, lower(hex(randomblob(24))) FROM nodes;
-- A fresh revision binds even an unedited draft to its node incarnation.
-- +goose StatementBegin
CREATE TRIGGER node_probe_plan_created AFTER INSERT ON nodes BEGIN
    INSERT INTO node_probe_plans(node_id, revision) VALUES(NEW.id, lower(hex(randomblob(24))));
END;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migration.
