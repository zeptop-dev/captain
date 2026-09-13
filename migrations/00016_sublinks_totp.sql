-- +goose Up
ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
CREATE TABLE sub_links (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL DEFAULT 'short',   -- short (permanent) | temp (limited)
    max_uses   INTEGER NOT NULL DEFAULT 0,      -- 0 = unlimited
    uses       INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX sub_links_user ON sub_links(user_id);

-- +goose Down
DROP TABLE sub_links;
ALTER TABLE users DROP COLUMN totp_enabled;
ALTER TABLE users DROP COLUMN totp_secret;
