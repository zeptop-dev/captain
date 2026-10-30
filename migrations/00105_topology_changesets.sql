-- +goose Up
-- Drafts contain bounded typed intent. Private creation settings never enter
-- catalogue, preview or history DTOs. Existing secrets remain live references.
CREATE TABLE topology_changesets (
 id TEXT PRIMARY KEY,
 version INTEGER NOT NULL,
 name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','committed')),
 operations_json TEXT NOT NULL,
 bindings_json TEXT NOT NULL DEFAULT '{}',
 result_json TEXT NOT NULL DEFAULT '[]',
 actor TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 committed_at INTEGER
);
CREATE INDEX topology_changesets_updated ON topology_changesets(updated_at DESC,id);
-- Object identity is independent of reusable SQLite integer IDs. Triggers cover
-- normal writes, imports and connection wizards without requiring every caller
-- to know about topology drafts.
CREATE TABLE topology_inbound_identities (
 inbound_id INTEGER PRIMARY KEY REFERENCES inbounds(id) ON DELETE CASCADE,
 identity TEXT NOT NULL
);
CREATE TABLE topology_entry_identities (
 entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
 identity TEXT NOT NULL
);
INSERT INTO topology_inbound_identities SELECT id,lower(hex(randomblob(16))) FROM inbounds;
INSERT INTO topology_entry_identities SELECT id,lower(hex(randomblob(16))) FROM entries;
-- +goose StatementBegin
CREATE TRIGGER topology_inbound_created AFTER INSERT ON inbounds BEGIN
 INSERT OR REPLACE INTO topology_inbound_identities VALUES(NEW.id,lower(hex(randomblob(16))));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER topology_entry_created AFTER INSERT ON entries BEGIN
 INSERT OR REPLACE INTO topology_entry_identities VALUES(NEW.id,lower(hex(randomblob(16))));
END;
-- +goose StatementEnd
CREATE TABLE topology_ingress_identities (
 ingress_id INTEGER PRIMARY KEY REFERENCES ingresses(id) ON DELETE CASCADE,
 identity TEXT NOT NULL
);
INSERT INTO topology_ingress_identities SELECT id,lower(hex(randomblob(16))) FROM ingresses;
-- +goose StatementBegin
CREATE TRIGGER topology_ingress_created AFTER INSERT ON ingresses BEGIN
 INSERT OR REPLACE INTO topology_ingress_identities VALUES(NEW.id,lower(hex(randomblob(16))));
END;
-- +goose StatementEnd
CREATE TABLE topology_line_identities (
 line_id INTEGER PRIMARY KEY REFERENCES forward_lines(id) ON DELETE CASCADE,
 identity TEXT NOT NULL
);
INSERT INTO topology_line_identities SELECT id,lower(hex(randomblob(16))) FROM forward_lines;
-- +goose StatementBegin
CREATE TRIGGER topology_line_created AFTER INSERT ON forward_lines BEGIN
 INSERT OR REPLACE INTO topology_line_identities VALUES(NEW.id,lower(hex(randomblob(16))));
END;
-- +goose StatementEnd
-- +goose Down
-- Forward-only: preserve committed operation identity and audit evidence.
