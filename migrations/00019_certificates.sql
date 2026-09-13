-- +goose Up
CREATE TABLE certificates (
    id         INTEGER PRIMARY KEY,
    domain     TEXT NOT NULL UNIQUE,                -- primary name (may be *.wildcard)
    names_json TEXT NOT NULL DEFAULT '[]',          -- every DNS name the leaf covers
    cert_pem   TEXT NOT NULL,
    key_pem    TEXT NOT NULL,
    not_after  INTEGER NOT NULL,
    source     TEXT NOT NULL DEFAULT 'upload',      -- upload | webhook
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE certificates;
