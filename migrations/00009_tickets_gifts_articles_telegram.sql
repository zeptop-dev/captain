-- +goose Up
CREATE TABLE tickets (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open',     -- open (waiting for admin) | replied (waiting for user) | closed
    priority      TEXT NOT NULL DEFAULT 'normal',   -- low | normal | high
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE INDEX tickets_user ON tickets(user_id, updated_at);
CREATE INDEX tickets_status ON tickets(status, updated_at);
CREATE TABLE ticket_messages (
    id          INTEGER PRIMARY KEY,
    ticket_id   INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    from_admin  INTEGER NOT NULL DEFAULT 0,
    body        TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX ticket_messages_ticket ON ticket_messages(ticket_id, id);

CREATE TABLE gift_codes (
    id           INTEGER PRIMARY KEY,
    code         TEXT NOT NULL UNIQUE,
    batch        TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL,                 -- balance (cents) | plan (plan_id + period_days) | traffic (bytes) | days
    value        INTEGER NOT NULL DEFAULT 0,
    plan_id      INTEGER,
    period_days  INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER,
    redeemed_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    redeemed_at  INTEGER,
    created_at   INTEGER NOT NULL
);
CREATE INDEX gift_codes_batch ON gift_codes(batch, id);

CREATE TABLE articles (
    id          INTEGER PRIMARY KEY,
    title       TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',       -- markdown; {{sub_url}} {{email}} {{site_name}} are substituted per user
    lang        TEXT NOT NULL DEFAULT '',       -- '' any, 'zh-CN', 'en'
    sort        INTEGER NOT NULL DEFAULT 0,
    published   INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

ALTER TABLE users ADD COLUMN telegram_id INTEGER;
CREATE UNIQUE INDEX users_telegram ON users(telegram_id) WHERE telegram_id IS NOT NULL;
CREATE TABLE telegram_bind_codes (
    code        TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE telegram_bind_codes;
DROP INDEX users_telegram;
ALTER TABLE users DROP COLUMN telegram_id;
DROP TABLE articles;
DROP TABLE gift_codes;
DROP TABLE ticket_messages;
DROP TABLE tickets;
