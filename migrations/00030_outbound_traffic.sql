-- +goose Up
CREATE TABLE outbound_traffic_daily (
    node_id    INTEGER NOT NULL,
    tag        TEXT NOT NULL,
    day        INTEGER NOT NULL,
    up_bytes   INTEGER NOT NULL DEFAULT 0,
    down_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, tag, day)
);

-- +goose Down
DROP TABLE outbound_traffic_daily;
