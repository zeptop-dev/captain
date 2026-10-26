-- +goose Up
-- Independent from the legacy clients setting, so old settings editors cannot
-- erase curated publications, cached metadata or retained upstream snapshots.
CREATE TABLE client_catalog (
    id TEXT PRIMARY KEY,
    document_json TEXT NOT NULL
);

-- +goose Down
-- Forward-only: retain published versions and catalog configuration.
