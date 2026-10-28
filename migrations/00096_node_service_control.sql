-- +goose Up
-- Separate private control state leaves saved listeners and user billing intact.
CREATE TABLE node_service_control (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    revision TEXT NOT NULL DEFAULT (lower(hex(randomblob(16)))),
    command_revision TEXT NOT NULL DEFAULT '',
    state_json TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO node_service_control(node_id) SELECT id FROM nodes;
-- A fresh incarnation prevents stale drafts from controlling a reused node ID.
-- +goose StatementBegin
CREATE TRIGGER node_service_control_create AFTER INSERT ON nodes BEGIN
    INSERT INTO node_service_control(node_id) VALUES(NEW.id);
END;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: do not discard operational restrictions on downgrade.
