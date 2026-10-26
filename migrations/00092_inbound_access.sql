-- +goose Up
CREATE TABLE inbound_access (
    inbound_id INTEGER PRIMARY KEY REFERENCES inbounds(id) ON DELETE CASCADE,
    policy TEXT NOT NULL CHECK(policy IN ('all', 'groups', 'none'))
);
CREATE TABLE inbound_access_groups (
    inbound_id INTEGER NOT NULL REFERENCES inbound_access(inbound_id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE RESTRICT,
    PRIMARY KEY(inbound_id, group_id)
);
CREATE INDEX inbound_access_groups_group ON inbound_access_groups(group_id, inbound_id);
-- Missing policy rows deliberately retain the legacy nullable group semantics.
CREATE VIEW effective_inbound_access AS
SELECT i.id AS inbound_id, CASE WHEN json_extract(i.settings_json,'$.reverse.receiver')=1 OR EXISTS(SELECT 1 FROM node_reverse_links r WHERE r.receiver_inbound_id=i.id) THEN 'none' ELSE COALESCE(a.policy, CASE WHEN i.group_id IS NULL THEN 'all' ELSE 'groups' END) END AS policy
FROM inbounds i LEFT JOIN inbound_access a ON a.inbound_id=i.id;
CREATE VIEW effective_inbound_access_groups AS
SELECT g.inbound_id,g.group_id FROM inbound_access_groups g JOIN inbound_access a ON a.inbound_id=g.inbound_id WHERE a.policy='groups'
UNION ALL
SELECT i.id,i.group_id FROM inbounds i WHERE i.group_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=i.id);

-- +goose Down
DROP VIEW effective_inbound_access_groups;
DROP VIEW effective_inbound_access;
DROP TABLE inbound_access_groups;
DROP TABLE inbound_access;
