-- +goose Up
CREATE TABLE identities (
    provider   TEXT NOT NULL,           -- oidc provider id from settings
    subject    TEXT NOT NULL,           -- the provider's stable user id (sub)
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    PRIMARY KEY (provider, subject)
);
CREATE INDEX identities_user ON identities(user_id);

-- +goose Down
DROP TABLE identities;
