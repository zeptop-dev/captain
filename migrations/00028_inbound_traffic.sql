-- +goose Up
CREATE TABLE inbound_traffic_daily (
    inbound_id INTEGER NOT NULL,
    day        INTEGER NOT NULL,
    up_bytes   INTEGER NOT NULL DEFAULT 0,
    down_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (inbound_id, day)
);

-- +goose Down
DROP TABLE inbound_traffic_daily;
