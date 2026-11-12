-- +goose Up
-- Keep an immutable receipt: later edits/deletions must not rewrite upgrade history.
CREATE TABLE plan_access_conversion_inbounds (inbound_id INTEGER PRIMARY KEY);
INSERT INTO plan_access_conversion_inbounds SELECT inbound_id FROM effective_inbound_access WHERE policy='all';
CREATE TABLE plan_access_conversion_plans (plan_id INTEGER PRIMARY KEY, group_id INTEGER, generated_name TEXT);
INSERT INTO plan_access_conversion_plans
 SELECT id,group_id,CASE WHEN group_id IS NULL THEN 'Plan ' || id || ' · ' || lower(hex(randomblob(12))) END FROM plans
 WHERE EXISTS(SELECT 1 FROM plan_access_conversion_inbounds);
INSERT INTO user_groups(name) SELECT generated_name FROM plan_access_conversion_plans WHERE group_id IS NULL;
UPDATE plan_access_conversion_plans SET group_id=(SELECT id FROM user_groups WHERE name=generated_name) WHERE group_id IS NULL;
UPDATE plans SET group_id=(SELECT group_id FROM plan_access_conversion_plans c WHERE c.plan_id=plans.id)
 WHERE group_id IS NULL AND id IN (SELECT plan_id FROM plan_access_conversion_plans);
INSERT INTO plan_access_ownership(plan_id,group_id)
 SELECT plan_id,group_id FROM plan_access_conversion_plans WHERE generated_name IS NOT NULL;
DELETE FROM inbound_access_groups WHERE inbound_id IN (SELECT inbound_id FROM plan_access_conversion_inbounds);
INSERT INTO inbound_access(inbound_id,policy)
 SELECT inbound_id,CASE WHEN EXISTS(SELECT 1 FROM plan_access_conversion_plans) THEN 'groups' ELSE 'none' END FROM plan_access_conversion_inbounds WHERE true
 ON CONFLICT(inbound_id) DO UPDATE SET policy=excluded.policy;
INSERT INTO inbound_access_groups(inbound_id,group_id)
 SELECT DISTINCT i.inbound_id,p.group_id FROM plan_access_conversion_inbounds i CROSS JOIN plan_access_conversion_plans p;
CREATE TABLE plan_access_conversion (id INTEGER PRIMARY KEY CHECK(id=1), converted_at INTEGER NOT NULL, inbound_count INTEGER NOT NULL, plan_count INTEGER NOT NULL);
INSERT INTO plan_access_conversion VALUES(1,unixepoch(),(SELECT count(*) FROM plan_access_conversion_inbounds),(SELECT count(*) FROM plan_access_conversion_plans));

-- Following is an explicit permission-scheme option; ordinary category assignment is a snapshot.
CREATE TABLE group_access_categories (
 group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
 category_id INTEGER NOT NULL REFERENCES entry_categories(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,category_id)
);
CREATE INDEX group_access_categories_category ON group_access_categories(category_id);
CREATE INDEX entries_category_inbound ON entries(category_id,inbound_id);
DROP VIEW effective_inbound_access;
DROP VIEW effective_inbound_access_groups;
CREATE VIEW effective_inbound_access_groups AS
 SELECT g.inbound_id,g.group_id FROM inbound_access_groups g JOIN inbound_access a ON a.inbound_id=g.inbound_id WHERE a.policy='groups'
 UNION
 SELECT i.id,i.group_id FROM inbounds i WHERE i.group_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=i.id)
 UNION
 SELECT e.inbound_id,g.group_id FROM entries e JOIN group_access_categories g ON g.category_id=e.category_id;
CREATE VIEW effective_inbound_access AS
 SELECT i.id AS inbound_id,
 CASE WHEN json_extract(i.settings_json,'$.reverse.receiver')=1 OR EXISTS(SELECT 1 FROM node_reverse_links r WHERE r.receiver_inbound_id=i.id) THEN 'none'
 WHEN COALESCE(a.policy,CASE WHEN i.group_id IS NULL THEN 'all' ELSE 'groups' END)='all' THEN 'all'
 WHEN EXISTS(SELECT 1 FROM effective_inbound_access_groups g WHERE g.inbound_id=i.id) THEN 'groups'
 ELSE 'none' END AS policy
 FROM inbounds i LEFT JOIN inbound_access a ON a.inbound_id=i.id;
-- Fence stale permission drafts even after category or entry A-to-B-to-A changes.
CREATE TABLE access_catalog_epoch (id INTEGER PRIMARY KEY CHECK(id=1), nonce TEXT NOT NULL);
INSERT INTO access_catalog_epoch VALUES(1,lower(hex(randomblob(16))));
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbounds_insert AFTER INSERT ON inbounds BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbounds_update AFTER UPDATE ON inbounds BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbounds_delete AFTER DELETE ON inbounds BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entries_insert AFTER INSERT ON entries BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entries_update AFTER UPDATE ON entries BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entries_delete AFTER DELETE ON entries BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entry_categories_insert AFTER INSERT ON entry_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entry_categories_update AFTER UPDATE ON entry_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_entry_categories_delete AFTER DELETE ON entry_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_user_groups_insert AFTER INSERT ON user_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_user_groups_update AFTER UPDATE ON user_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_user_groups_delete AFTER DELETE ON user_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_plans_insert AFTER INSERT ON plans BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_plans_update AFTER UPDATE ON plans BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_plans_delete AFTER DELETE ON plans BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_insert AFTER INSERT ON inbound_access BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_update AFTER UPDATE ON inbound_access BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_delete AFTER DELETE ON inbound_access BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_groups_insert AFTER INSERT ON inbound_access_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_groups_update AFTER UPDATE ON inbound_access_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_inbound_access_groups_delete AFTER DELETE ON inbound_access_groups BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_group_access_categories_insert AFTER INSERT ON group_access_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_group_access_categories_update AFTER UPDATE ON group_access_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER access_epoch_group_access_categories_delete AFTER DELETE ON group_access_categories BEGIN
 UPDATE access_catalog_epoch SET nonce=lower(hex(randomblob(16))) WHERE id=1;
END;
-- +goose StatementEnd
-- +goose Down
-- Forward-only migration.
