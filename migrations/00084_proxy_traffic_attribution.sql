-- +goose Up
-- Node IDs may be reused after deletion. A retained attribution must not
-- become the history of a newly created server with the same node ID.
CREATE TABLE node_proxy_identities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id INTEGER UNIQUE REFERENCES nodes(id) ON DELETE SET NULL,
    attribution_started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO node_proxy_identities(node_id) SELECT id FROM nodes;
-- +goose StatementBegin
CREATE TRIGGER node_proxy_identity_create AFTER INSERT ON nodes BEGIN
    INSERT INTO node_proxy_identities(node_id) VALUES(NEW.id);
END;
-- +goose StatementEnd

-- This is a projection of customer proxy bytes, not another customer bill.
-- Neither inbound nor connection deletion changes an accepted attribution.
CREATE TABLE node_proxy_traffic_daily (
    collector_id INTEGER NOT NULL REFERENCES node_proxy_identities(id),
    exit_id INTEGER NOT NULL REFERENCES node_proxy_identities(id),
    day INTEGER NOT NULL,
    up_bytes INTEGER NOT NULL DEFAULT 0 CHECK(up_bytes >= 0),
    down_bytes INTEGER NOT NULL DEFAULT 0 CHECK(down_bytes >= 0),
    unattributed_bytes INTEGER NOT NULL DEFAULT 0 CHECK(unattributed_bytes >= 0),
    PRIMARY KEY(collector_id, exit_id, day)
);
CREATE INDEX node_proxy_traffic_daily_day ON node_proxy_traffic_daily(day);

-- Only an existing inbound with history strictly after its creation can be
-- identified safely: old daily rows lack timestamps and inbound IDs may be
-- reused. Do not infer the topology of deleted inbounds or creation-day rows.
INSERT INTO node_proxy_traffic_daily(collector_id, exit_id, day, up_bytes, down_bytes)
SELECT c.id, e.id, t.day, SUM(t.up_bytes), SUM(t.down_bytes)
FROM traffic_daily t
JOIN inbounds i ON i.id = t.inbound_id
JOIN node_proxy_identities c ON c.node_id = i.node_id
LEFT JOIN node_wireguard_links w ON w.user_inbound_id = i.id AND w.transit_id = i.node_id
LEFT JOIN node_reverse_links r ON r.user_inbound_id = i.id AND r.transit_id = i.node_id
JOIN node_proxy_identities e ON e.node_id = COALESCE(w.exit_id, r.exit_id, i.node_id)
WHERE t.day >= i.created_at AND t.up_bytes >= 0 AND t.down_bytes >= 0
GROUP BY c.id, e.id, t.day;

-- +goose Down
DROP TRIGGER node_proxy_identity_create;
DROP TABLE node_proxy_traffic_daily;
DROP TABLE node_proxy_identities;
