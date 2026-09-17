-- +goose Up
-- The last traffic batch a node's report was applied for, so a re-sent
-- batch (a report whose response was lost) is not charged twice.
ALTER TABLE nodes ADD COLUMN traffic_seq INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nodes DROP COLUMN traffic_seq;
