-- +goose Up
CREATE TABLE entry_categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    sort INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE entries ADD COLUMN category_id INTEGER REFERENCES entry_categories(id) ON DELETE SET NULL;
CREATE INDEX idx_entries_category_id ON entries(category_id);

-- +goose Down
-- Forward-only migration.
