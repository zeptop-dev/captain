-- +goose Up
CREATE TABLE forward_paths (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL,
 version INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE forward_path_revisions (
 path_id INTEGER NOT NULL REFERENCES forward_paths(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL,
 definition_json TEXT NOT NULL,
 hops_json TEXT NOT NULL,
 depth INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(path_id,revision)
);
CREATE TABLE forward_path_hops (
 path_id INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 hop_key TEXT NOT NULL,
 node_id INTEGER NOT NULL REFERENCES nodes(id),
 ingress_id INTEGER REFERENCES ingresses(id),
 PRIMARY KEY(path_id,revision,hop_key),
 FOREIGN KEY(path_id,revision) REFERENCES forward_path_revisions(path_id,revision) ON DELETE CASCADE
);
-- Keep only source versions that a path revision actually pins. These snapshots
-- contain hop identities and address policy, never inbound credentials.
CREATE TABLE forward_path_line_versions (
 line_id INTEGER NOT NULL REFERENCES forward_lines(id) ON DELETE CASCADE,
 version INTEGER NOT NULL,
 include_landing INTEGER NOT NULL,
 hops_json TEXT NOT NULL,
 PRIMARY KEY(line_id,version,include_landing)
);
CREATE TABLE forward_path_refs (
 path_id INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 step_id TEXT NOT NULL,
 source_path_id INTEGER,
 source_revision INTEGER,
 source_line_id INTEGER REFERENCES forward_lines(id),
 source_line_version INTEGER,
 source_include_landing INTEGER,
 PRIMARY KEY(path_id,revision,step_id),
 FOREIGN KEY(path_id,revision) REFERENCES forward_path_revisions(path_id,revision) ON DELETE CASCADE,
 FOREIGN KEY(source_path_id,source_revision) REFERENCES forward_path_revisions(path_id,revision),
 FOREIGN KEY(source_line_id,source_line_version,source_include_landing) REFERENCES forward_path_line_versions(line_id,version,include_landing)
);
CREATE TABLE forward_path_routes (
 line_id INTEGER PRIMARY KEY REFERENCES forward_lines(id) ON DELETE CASCADE,
 spec_json TEXT NOT NULL
);
CREATE TABLE forward_path_route_refs (
 line_id INTEGER NOT NULL REFERENCES forward_path_routes(line_id) ON DELETE CASCADE,
 branch_id TEXT NOT NULL,
 path_id INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 PRIMARY KEY(line_id,branch_id),
 FOREIGN KEY(path_id,revision) REFERENCES forward_path_revisions(path_id,revision)
);
CREATE INDEX forward_path_hops_node ON forward_path_hops(node_id);
CREATE INDEX forward_path_refs_line ON forward_path_refs(source_line_id);
CREATE INDEX forward_path_refs_path ON forward_path_refs(source_path_id);
CREATE INDEX forward_path_route_refs_path ON forward_path_route_refs(path_id);
-- +goose Down
-- Forward-only: immutable path references and owned resources must be retained.
