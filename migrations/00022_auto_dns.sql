-- +goose Up
ALTER TABLE domains ADD COLUMN auto_dns INTEGER NOT NULL DEFAULT 1;
ALTER TABLE ingresses ADD COLUMN entry_domain TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE ingresses DROP COLUMN entry_domain;
ALTER TABLE domains DROP COLUMN auto_dns;
