-- +goose Up
-- Public UUIDs belong to a node incarnation, not its reusable SQLite row ID.
CREATE TABLE probe_public_ids (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    uuid TEXT NOT NULL UNIQUE DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-a' || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6))))
);
INSERT INTO probe_public_ids(node_id) SELECT id FROM nodes;
-- +goose StatementBegin
CREATE TRIGGER probe_public_ids_create AFTER INSERT ON nodes BEGIN
    INSERT INTO probe_public_ids(node_id) VALUES(NEW.id);
END;
-- +goose StatementEnd

-- Metric rollups preserve sufficient statistics instead of re-averaging averages.
-- Values contain only the fixed public metric vocabulary, never device paths.
CREATE TABLE node_probe_metric_stats (
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    res TEXT NOT NULL,
    ts INTEGER NOT NULL,
    values_json TEXT NOT NULL,
    PRIMARY KEY(node_id, res, ts)
);

-- +goose Down
-- Forward only: old public identifiers must never be reassigned.
