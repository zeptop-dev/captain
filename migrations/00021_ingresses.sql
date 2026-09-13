-- +goose Up
CREATE TABLE ingresses (
    id          INTEGER PRIMARY KEY,
    node_id     INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'mapped',   -- mapped (a line/NAT in front) ; direct is implicit
    bind_ip     TEXT NOT NULL DEFAULT '',         -- local address inbounds bind to ('' = all)
    line_ip     TEXT NOT NULL DEFAULT '',         -- the line's far-end address a relay forwards to
    entry_host  TEXT NOT NULL DEFAULT '',         -- provider-supplied public entry clients dial ('' = none)
    port_from   INTEGER NOT NULL DEFAULT 0,       -- usable port range on the line (0 = any)
    port_to     INTEGER NOT NULL DEFAULT 0,
    port_offset INTEGER NOT NULL DEFAULT 0,       -- entry port = local port + offset
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX ingresses_node ON ingresses(node_id);
ALTER TABLE inbounds ADD COLUMN ingress_id INTEGER REFERENCES ingresses(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE inbounds DROP COLUMN ingress_id;
DROP TABLE ingresses;
