-- +goose Up
-- Entries hidden from one user (per-user blacklist on top of group rules).
CREATE TABLE user_entry_blocks (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, entry_id)
);

-- +goose Down
DROP TABLE user_entry_blocks;
