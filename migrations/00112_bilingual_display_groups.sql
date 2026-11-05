-- +goose Up
ALTER TABLE node_display_groups ADD COLUMN name_zh TEXT NOT NULL DEFAULT '';
ALTER TABLE node_display_groups ADD COLUMN name_en TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_categories ADD COLUMN name_zh TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_categories ADD COLUMN name_en TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Forward-only migration.
