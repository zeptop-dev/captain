-- +goose Up
ALTER TABLE nodes ADD COLUMN address_fallback_auto_v4 INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN address_fallback_auto_v6 INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN address_fallback_changed_v4 INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN address_fallback_changed_v6 INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Forward-only migration.
