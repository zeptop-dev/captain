-- +goose Up
-- API tokens gain a scope and an optional expiry: a token handed to a
-- script or an agent should be able to read without being able to change
-- anything, and should be able to stop working on its own.
ALTER TABLE api_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT 'full';
ALTER TABLE api_tokens ADD COLUMN expires_at INTEGER;

-- +goose Down
ALTER TABLE api_tokens DROP COLUMN scope;
ALTER TABLE api_tokens DROP COLUMN expires_at;
