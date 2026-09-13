-- +goose Up
ALTER TABLE nodes ADD COLUMN outbounds_json TEXT NOT NULL DEFAULT '[]';   -- []spec.Outbound (landing servers)
ALTER TABLE nodes ADD COLUMN routes_json TEXT NOT NULL DEFAULT '[]';      -- []spec.RouteRule
ALTER TABLE nodes ADD COLUMN default_outbound TEXT NOT NULL DEFAULT '';   -- tag, '' = direct

CREATE TABLE external_sources (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    url          TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT 'v2rayN/7.0',
    group_id     INTEGER REFERENCES user_groups(id) ON DELETE SET NULL,
    rate         REAL NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    last_sync_at INTEGER,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);
CREATE TABLE external_nodes (
    id           INTEGER PRIMARY KEY,
    source_id    INTEGER REFERENCES external_sources(id) ON DELETE CASCADE,  -- NULL = added by hand
    name         TEXT NOT NULL,
    uri          TEXT NOT NULL,
    group_id     INTEGER REFERENCES user_groups(id) ON DELETE SET NULL,
    rate         REAL NOT NULL DEFAULT 1,
    sort         INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX external_nodes_source ON external_nodes(source_id);

-- +goose Down
DROP TABLE external_nodes;
DROP TABLE external_sources;
ALTER TABLE nodes DROP COLUMN default_outbound;
ALTER TABLE nodes DROP COLUMN routes_json;
ALTER TABLE nodes DROP COLUMN outbounds_json;
