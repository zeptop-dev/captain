-- +goose Up
-- Each lineage survives deletion; a reused numeric line ID gets a fresh scope.
CREATE TABLE line_change_scopes (
    ordinal INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE DEFAULT (lower(hex(randomblob(16)))),
    line_id INTEGER UNIQUE REFERENCES forward_lines(id) ON DELETE SET NULL,
    original_line_id INTEGER NOT NULL,
    latest_change_id TEXT NOT NULL DEFAULT ''
);
INSERT INTO line_change_scopes(line_id,original_line_id) SELECT id,id FROM forward_lines;
-- +goose StatementBegin
CREATE TRIGGER line_change_scope_create AFTER INSERT ON forward_lines BEGIN
    INSERT INTO line_change_scopes(line_id,original_line_id) VALUES(NEW.id,NEW.id);
END;
-- +goose StatementEnd
CREATE TABLE line_changes (
    ordinal INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    scope_id TEXT NOT NULL REFERENCES line_change_scopes(id) ON DELETE CASCADE,
    line_id INTEGER NOT NULL,
    version INTEGER NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    actor TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    superseded_at INTEGER
);
CREATE INDEX line_changes_history ON line_changes(scope_id,ordinal DESC);
CREATE TABLE line_change_targets (
    change_id TEXT NOT NULL REFERENCES line_changes(id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL REFERENCES line_change_scopes(id) ON DELETE CASCADE,
    node_id INTEGER NOT NULL,
    node_identity TEXT NOT NULL,
    node_name TEXT NOT NULL,
    token_identity TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    before_json TEXT NOT NULL,
    after_json TEXT NOT NULL,
    expected_digest TEXT NOT NULL,
    superseded_at INTEGER,
    delivery_id TEXT NOT NULL DEFAULT '',
    session TEXT NOT NULL DEFAULT '',
    config_revision TEXT NOT NULL DEFAULT '',
    state_revision TEXT NOT NULL DEFAULT '',
    issued_at INTEGER,
    confirmed_at INTEGER,
    observed_at INTEGER,
    last_receipt_at INTEGER,
    receipt_sequence INTEGER NOT NULL DEFAULT 0,
    applied INTEGER NOT NULL DEFAULT 0 CHECK(applied IN (0,1)),
    PRIMARY KEY(change_id,node_id)
);
CREATE INDEX line_change_targets_current ON line_change_targets(node_id,scope_id,superseded_at);
-- No FK to nodes or node_config_deliveries: finite copied evidence survives
-- node removal and the independent 100-delivery retention window.

-- Candidate mappings cannot displace active evidence. They are bounded by the
-- existing delivery window and vanish when either side is pruned.
CREATE TABLE line_change_deliveries (
    delivery_id TEXT NOT NULL REFERENCES node_config_deliveries(id) ON DELETE CASCADE,
    change_id TEXT NOT NULL,
    node_id INTEGER NOT NULL,
    PRIMARY KEY(delivery_id,change_id,node_id),
    FOREIGN KEY(change_id,node_id) REFERENCES line_change_targets(change_id,node_id) ON DELETE CASCADE
);

-- Numeric IDs may be manually reused after a deletion. Only the new scope may
-- track that prefix from then on; old immutable history remains addressable.
-- +goose StatementBegin
CREATE TRIGGER line_change_scope_supersede AFTER INSERT ON line_change_scopes BEGIN
    UPDATE line_change_targets SET superseded_at=CAST(strftime('%s','now') AS INTEGER)*1000
    WHERE superseded_at IS NULL AND scope_id IN
      (SELECT id FROM line_change_scopes WHERE original_line_id=NEW.original_line_id AND id<>NEW.id);
    UPDATE line_changes SET superseded_at=CAST(strftime('%s','now') AS INTEGER)*1000
    WHERE superseded_at IS NULL AND scope_id IN
      (SELECT id FROM line_change_scopes WHERE original_line_id=NEW.original_line_id AND id<>NEW.id)
      AND EXISTS(SELECT 1 FROM line_change_targets t WHERE t.change_id=line_changes.id);
END;
-- +goose StatementEnd

-- +goose Down
-- Forward only: retain immutable change identities and target evidence.
