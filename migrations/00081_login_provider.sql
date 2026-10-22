-- +goose Up
CREATE TABLE login_clients (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 secret_hash TEXT NOT NULL,
 redirect_uri TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE login_subjects (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 subject TEXT NOT NULL UNIQUE
);
CREATE TABLE login_requests (
 hash TEXT PRIMARY KEY,
 client_id TEXT NOT NULL REFERENCES login_clients(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 stage TEXT NOT NULL,
 redirect_uri TEXT NOT NULL,
 state TEXT NOT NULL,
 scope TEXT NOT NULL,
 challenge TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX login_requests_expiry ON login_requests(expires_at);
-- +goose Down
DROP TABLE login_requests;
DROP TABLE login_subjects;
DROP TABLE login_clients;
