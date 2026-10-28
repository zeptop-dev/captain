-- +goose Up
-- Immutable release packages and independent settings are included in database
-- backups. Themes never create files or execute code on the Captain server.
CREATE TABLE probe_theme_packages (
    digest TEXT PRIMARY KEY CHECK(length(digest) = 64),
    short TEXT NOT NULL,
    version TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    archive BLOB NOT NULL CHECK(length(archive) > 0 AND length(archive) <= 33554432),
    settings_json TEXT NOT NULL DEFAULT '{}',
    revision INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    UNIQUE(short, version)
);
CREATE TABLE probe_theme_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    active_digest TEXT REFERENCES probe_theme_packages(digest) ON DELETE RESTRICT,
    revision INTEGER NOT NULL DEFAULT 0
);
INSERT INTO probe_theme_state (id) VALUES (1);
