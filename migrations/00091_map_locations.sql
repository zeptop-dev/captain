-- +goose Up
-- Presentation metadata is separate from Entry routing and subscription region.
CREATE TABLE entry_locations (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    settings_json TEXT NOT NULL
);
CREATE TABLE site_map_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    revision INTEGER NOT NULL DEFAULT 0 CHECK(revision >= 0)
);
CREATE TABLE site_map_entries (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    position INTEGER NOT NULL
);
-- Retire old browser drafts even when a deleted SQLite Entry ID is reused.
-- +goose StatementBegin
CREATE TRIGGER site_map_entry_retired AFTER DELETE ON entries BEGIN
    INSERT INTO site_map_state(id, revision) VALUES(1, 1)
    ON CONFLICT(id) DO UPDATE SET revision = revision + 1;
END;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: preserve manual locations and explicit publication references.
