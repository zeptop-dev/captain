-- +goose Up
CREATE TABLE verification_codes (
    email      TEXT NOT NULL,
    purpose    TEXT NOT NULL,           -- 'register' | 'reset'
    code_hash  TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (email, purpose)
);
CREATE TABLE notifications (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL,              -- 'expiry' | 'traffic'
    ref     TEXT NOT NULL,              -- what the reminder was about (expiry time / period), so it is sent once
    sent_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, kind, ref)
);

-- +goose Down
DROP TABLE notifications;
DROP TABLE verification_codes;
