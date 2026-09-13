-- +goose Up
CREATE TABLE domains (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,                -- registrable domain, e.g. example.com
    provider   TEXT NOT NULL DEFAULT 'cloudflare',  -- cloudflare | manual
    cf_token   TEXT NOT NULL DEFAULT '',            -- per-domain Cloudflare token; '' = the ACME settings token
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
ALTER TABLE certificates ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN renewals INTEGER NOT NULL DEFAULT 0;
ALTER TABLE certificates ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN auto_renew INTEGER NOT NULL DEFAULT 1;
ALTER TABLE certificates ADD COLUMN domain_id INTEGER;
ALTER TABLE nodes ADD COLUMN domain TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN domain;
ALTER TABLE certificates DROP COLUMN domain_id;
ALTER TABLE certificates DROP COLUMN auto_renew;
ALTER TABLE certificates DROP COLUMN last_error;
ALTER TABLE certificates DROP COLUMN renewals;
ALTER TABLE certificates DROP COLUMN issuer;
ALTER TABLE certificates DROP COLUMN name;
DROP TABLE domains;
