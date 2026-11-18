-- +goose Up
-- Preserve the never-reused slot counter even when the highest link was deleted.
CREATE TEMP TABLE wg_slot_counter AS SELECT seq FROM sqlite_sequence WHERE name='node_wireguard_links';
DROP TRIGGER wireguard_link_cleanup;
CREATE TABLE node_wireguard_links_new (
    slot INTEGER PRIMARY KEY AUTOINCREMENT CHECK(slot BETWEEN 1 AND 16383),
    id TEXT NOT NULL UNIQUE,
    exit_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    transit_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    user_inbound_id INTEGER UNIQUE REFERENCES inbounds(id) ON DELETE CASCADE,
    entry_id INTEGER UNIQUE REFERENCES entries(id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1,
    transit_key TEXT NOT NULL,
    exit_key TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    listen_port INTEGER NOT NULL CHECK(listen_port BETWEEN 1 AND 65535),
    mtu INTEGER NOT NULL CHECK(mtu BETWEEN 1280 AND 1420),
    domain_preference TEXT NOT NULL DEFAULT '',
    auto_forwarding INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    private_access_json TEXT NOT NULL DEFAULT 'null',
    CHECK(exit_id <> transit_id),
    CHECK((user_inbound_id IS NULL) = (entry_id IS NULL))
);
INSERT INTO node_wireguard_links_new SELECT w.slot,w.id,w.exit_id,w.transit_id,w.user_inbound_id,w.entry_id,w.version,w.transit_key,w.exit_key,w.endpoint,w.listen_port,w.mtu,w.domain_preference,w.auto_forwarding,e.name,i.enabled,COALESCE(json_extract(i.settings_json,'$.private_access'),'null') FROM node_wireguard_links w JOIN inbounds i ON i.id=w.user_inbound_id JOIN entries e ON e.id=w.entry_id;
DROP TABLE node_wireguard_links;
ALTER TABLE node_wireguard_links_new RENAME TO node_wireguard_links;
INSERT INTO sqlite_sequence(name,seq) SELECT 'node_wireguard_links',seq FROM wg_slot_counter WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='node_wireguard_links');
UPDATE sqlite_sequence SET seq=MAX(seq,COALESCE((SELECT seq FROM wg_slot_counter),0)) WHERE name='node_wireguard_links';
DROP TABLE wg_slot_counter;
CREATE TABLE wireguard_bindings (
    inbound_id INTEGER PRIMARY KEY REFERENCES inbounds(id) ON DELETE RESTRICT,
    link_id TEXT NOT NULL REFERENCES node_wireguard_links(id) ON DELETE RESTRICT
);
CREATE INDEX wireguard_bindings_link ON wireguard_bindings(link_id);
CREATE VIEW wireguard_inbound_links AS
 SELECT w.*,w.user_inbound_id AS inbound_id FROM node_wireguard_links w WHERE w.user_inbound_id IS NOT NULL
 UNION ALL
 SELECT w.*,b.inbound_id FROM node_wireguard_links w JOIN wireguard_bindings b ON b.link_id=w.id;
-- Legacy connection deletion keeps its original owned-resource semantics.
-- +goose StatementBegin
CREATE TRIGGER wireguard_link_cleanup AFTER DELETE ON node_wireguard_links BEGIN
    DELETE FROM inbounds WHERE id=OLD.user_inbound_id;
    DELETE FROM entries WHERE id=OLD.entry_id;
END;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
