-- +goose Up
ALTER TABLE entries ADD COLUMN client_extra TEXT NOT NULL DEFAULT '';          -- JSON object merged into this entry's Clash/Stash/sing-box proxy
ALTER TABLE nodes ADD COLUMN mita_quotas INTEGER NOT NULL DEFAULT 0;           -- write user allowances into mita's own quotas
ALTER TABLE external_sources ADD COLUMN hide_dead INTEGER NOT NULL DEFAULT 0;  -- drop nodes whose last probe failed from subscriptions
ALTER TABLE external_nodes ADD COLUMN probed_at INTEGER;                       -- last TCP probe from the panel
ALTER TABLE external_nodes ADD COLUMN probe_ms REAL NOT NULL DEFAULT -1;       -- best latency, -1 = unreachable / never
ALTER TABLE external_nodes ADD COLUMN probe_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE external_nodes DROP COLUMN probe_error;
ALTER TABLE external_nodes DROP COLUMN probe_ms;
ALTER TABLE external_nodes DROP COLUMN probed_at;
ALTER TABLE external_sources DROP COLUMN hide_dead;
ALTER TABLE nodes DROP COLUMN mita_quotas;
ALTER TABLE entries DROP COLUMN client_extra;
