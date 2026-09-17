-- +goose Up
ALTER TABLE nodes ADD COLUMN egress_by_ingress INTEGER NOT NULL DEFAULT 0; -- inbounds bound to an address exit from it

-- +goose Down
ALTER TABLE nodes DROP COLUMN egress_by_ingress;
