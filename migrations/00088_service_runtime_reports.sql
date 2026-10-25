-- +goose Up
-- Resource-only heartbeats must not make old proxy runtime observations fresh.
ALTER TABLE nodes ADD COLUMN runtime_reported_at INTEGER;
ALTER TABLE nodes ADD COLUMN runtime_report_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN runtime_report_json TEXT NOT NULL DEFAULT 'null';

-- Publication references follow the lifetime of an Entry, not its reusable
-- SQLite row ID. All deletion paths (including inbound/link cascades) clear
-- these references and invalidate drafts before another Entry can reuse an ID.
CREATE TABLE service_status_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    public_enabled INTEGER NOT NULL DEFAULT 0 CHECK(public_enabled IN (0, 1)),
    revision INTEGER NOT NULL DEFAULT 0 CHECK(revision >= 0)
);
CREATE TABLE service_status_public_entries (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    position INTEGER NOT NULL
);
CREATE TABLE service_status_maintenance (
    entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    starts_at INTEGER NOT NULL,
    ends_at INTEGER NOT NULL,
    note TEXT NOT NULL DEFAULT ''
);
-- +goose StatementBegin
CREATE TRIGGER service_status_entry_retired AFTER DELETE ON entries BEGIN
    INSERT INTO service_status_settings(id, public_enabled, revision) VALUES(1, 0, 1)
    ON CONFLICT(id) DO UPDATE SET revision = revision + 1;
END;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: retain historical observations and publication references.
