-- +goose Up
-- The tables the node reports fill were not cleaned up when a user or a
-- node was deleted, and SQLite reuses a rowid, so the next registrant
-- could inherit somebody's connection log, audit hits and throttle.
DELETE FROM conn_log WHERE user_id NOT IN (SELECT id FROM users) OR node_id NOT IN (SELECT id FROM nodes);
DELETE FROM audit_log WHERE user_id NOT IN (SELECT id FROM users) OR node_id NOT IN (SELECT id FROM nodes);
DELETE FROM dyn_limits WHERE user_id NOT IN (SELECT id FROM users);
DELETE FROM hwid_devices WHERE user_id NOT IN (SELECT id FROM users);
DELETE FROM sub_requests WHERE user_id NOT IN (SELECT id FROM users);

-- first_connected_at was added empty, so every existing customer would
-- have fired "first connected" on their next byte: take the oldest day
-- they were ever charged for instead.
UPDATE users SET first_connected_at = (
    SELECT MIN(day) FROM traffic_daily t WHERE t.user_id = users.id
) WHERE first_connected_at IS NULL AND EXISTS (SELECT 1 FROM traffic_daily t WHERE t.user_id = users.id);

-- +goose Down
SELECT 1;
