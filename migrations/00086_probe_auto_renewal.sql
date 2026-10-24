-- +goose Up
-- A bounded, private replay cursor for expiry maintenance; not billing history.
ALTER TABLE nodes ADD COLUMN probe_renewal_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE nodes DROP COLUMN probe_renewal_json;
