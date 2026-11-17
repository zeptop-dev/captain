-- +goose Up
CREATE TABLE user_email_verifications (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 email TEXT NOT NULL, source TEXT NOT NULL, verified_at INTEGER NOT NULL
);
CREATE TABLE registration_receipts (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 version TEXT NOT NULL, terms_url TEXT NOT NULL, privacy_url TEXT NOT NULL,
 document_version TEXT NOT NULL, method TEXT NOT NULL, accepted_at INTEGER NOT NULL
);
CREATE TABLE registration_oauth_consents (
 state_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, version TEXT NOT NULL,
 accepted_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER user_email_verification_invalidated AFTER UPDATE OF email ON users
WHEN NEW.email <> OLD.email
BEGIN
 DELETE FROM user_email_verifications WHERE user_id=NEW.id;
END;
-- +goose StatementEnd
-- +goose Down
SELECT 1;
