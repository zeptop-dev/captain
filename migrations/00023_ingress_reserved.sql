-- +goose Up
ALTER TABLE ingresses ADD COLUMN reserved_ports TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE ingresses DROP COLUMN reserved_ports;
