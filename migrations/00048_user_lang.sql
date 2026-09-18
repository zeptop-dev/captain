-- +goose Up
-- The language a user reads. Set from the portal (it follows the
-- interface language the user picked) and used for the mail we send them,
-- so a mixed audience gets each message in its own language. Empty means
-- "use the panel's mail language".
ALTER TABLE users ADD COLUMN lang TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN lang;
