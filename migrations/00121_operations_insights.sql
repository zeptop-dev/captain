-- +goose Up
ALTER TABLE ping_tasks ADD COLUMN alert_changed_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE node_route_watch (
 node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 policy_json TEXT NOT NULL DEFAULT '{}', updated_at INTEGER NOT NULL
);
CREATE TABLE node_route_states (
 node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 path_key TEXT NOT NULL, state_json TEXT NOT NULL, PRIMARY KEY(node_id,path_key)
);
CREATE TABLE node_route_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 job_id TEXT NOT NULL, path_key TEXT NOT NULL, at INTEGER NOT NULL, event_json TEXT NOT NULL,
 UNIQUE(node_id,job_id,path_key)
);
CREATE INDEX node_route_events_history ON node_route_events(node_id,id DESC);
CREATE TABLE node_network_alerts (
 node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 policy_json TEXT NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE node_network_baselines (
 node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 series_key TEXT NOT NULL, state_json TEXT NOT NULL, PRIMARY KEY(node_id,series_key)
);
CREATE TABLE monitor_kind_silences (
 node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, until_at INTEGER NOT NULL, PRIMARY KEY(node_id,kind)
);
CREATE TABLE infra_price_history (
 id INTEGER PRIMARY KEY AUTOINCREMENT, asset_id INTEGER NOT NULL REFERENCES infra_assets(id) ON DELETE CASCADE,
 effective_at INTEGER NOT NULL, currency TEXT NOT NULL, amount_minor INTEGER NOT NULL,
 period_months INTEGER NOT NULL, next_due TEXT NOT NULL, anchor_day INTEGER NOT NULL, active INTEGER NOT NULL
);
INSERT INTO infra_price_history(asset_id,effective_at,currency,amount_minor,period_months,next_due,anchor_day,active)
 SELECT id,unixepoch(),currency,amount_minor,period_months,next_due,anchor_day,active FROM infra_assets;
-- +goose StatementBegin
CREATE TRIGGER infra_price_created AFTER INSERT ON infra_assets BEGIN
 INSERT INTO infra_price_history(asset_id,effective_at,currency,amount_minor,period_months,next_due,anchor_day,active)
 VALUES(NEW.id,unixepoch(),NEW.currency,NEW.amount_minor,NEW.period_months,NEW.next_due,NEW.anchor_day,NEW.active);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER infra_price_changed AFTER UPDATE ON infra_assets
 WHEN OLD.currency<>NEW.currency OR OLD.amount_minor<>NEW.amount_minor OR OLD.period_months<>NEW.period_months
 OR OLD.next_due<>NEW.next_due OR OLD.anchor_day<>NEW.anchor_day OR OLD.active<>NEW.active BEGIN
 INSERT INTO infra_price_history(asset_id,effective_at,currency,amount_minor,period_months,next_due,anchor_day,active)
 VALUES(NEW.id,unixepoch(),NEW.currency,NEW.amount_minor,NEW.period_months,NEW.next_due,NEW.anchor_day,NEW.active);
END;
-- +goose StatementEnd
CREATE INDEX infra_price_history_asset ON infra_price_history(asset_id,effective_at,id);
CREATE TABLE infra_extra_expenses (
 id INTEGER PRIMARY KEY AUTOINCREMENT, asset_id INTEGER NOT NULL REFERENCES infra_assets(id),
 request_key TEXT NOT NULL UNIQUE, currency TEXT NOT NULL, amount_minor INTEGER NOT NULL,
 paid_date TEXT NOT NULL, category TEXT NOT NULL, note TEXT NOT NULL, created_at INTEGER NOT NULL,
 reversal_of INTEGER UNIQUE REFERENCES infra_extra_expenses(id)
);
-- +goose Down
SELECT 1;
