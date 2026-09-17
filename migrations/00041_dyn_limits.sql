-- +goose Up
-- Temporary speed limits the dynamic limiter put on users.
CREATE TABLE dyn_limits (
    user_id INTEGER PRIMARY KEY,
    mbps    INTEGER NOT NULL,
    since   INTEGER NOT NULL,
    until   INTEGER NOT NULL,
    rate    INTEGER NOT NULL DEFAULT 0  -- the Mbps that triggered it
);
CREATE INDEX dyn_limits_until ON dyn_limits (until);

-- +goose Down
DROP TABLE dyn_limits;
