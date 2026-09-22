-- +goose Up
-- The node's server id in a DStatus panel. Active mode (the node reports
-- instead of being scraped) has to name the server it reports as, and
-- that id only exists in the panel, so it is copied here per node.
ALTER TABLE nodes ADD COLUMN dstatus_sid TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN dstatus_sid;
