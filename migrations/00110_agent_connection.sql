-- +goose Up
-- These settings generate future installs. They do not redirect running agents.
ALTER TABLE nodes ADD COLUMN agent_url TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN download_source TEXT NOT NULL DEFAULT 'github' CHECK(download_source IN ('github','captain'));

-- +goose Down
-- Forward-only: preserve explicit agent installation choices.
