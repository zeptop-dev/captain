-- +goose Up
ALTER TABLE users ADD COLUMN register_ip TEXT NOT NULL DEFAULT '';
CREATE INDEX users_register_ip ON users(register_ip, created_at);

-- +goose Down
DROP INDEX users_register_ip;
ALTER TABLE users DROP COLUMN register_ip;
