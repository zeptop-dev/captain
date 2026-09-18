-- +goose Up
-- Who changed what in the console. The role matrix gives operators and
-- support their own accounts, so there has to be a record of what each
-- account did: every non-GET admin request lands here with the actor, the
-- route, the target and the outcome.
CREATE TABLE admin_log (
    id         INTEGER PRIMARY KEY,
    at         INTEGER NOT NULL,
    user_id    INTEGER,                        -- staff account; NULL if the session was already gone
    email      TEXT NOT NULL DEFAULT '',       -- kept even when the account is later deleted
    role       TEXT NOT NULL DEFAULT '',
    method     TEXT NOT NULL,
    path       TEXT NOT NULL,                  -- subscription tokens are never part of an admin path
    target     TEXT NOT NULL DEFAULT '',       -- the {id} of the route, when it has one
    status     INTEGER NOT NULL DEFAULT 0,
    via        TEXT NOT NULL DEFAULT '',       -- "session" or "token"
    ip         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX admin_log_at ON admin_log(at DESC);
CREATE INDEX admin_log_user ON admin_log(user_id, at DESC);

-- +goose Down
DROP TABLE admin_log;
