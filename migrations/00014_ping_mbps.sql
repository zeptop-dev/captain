-- +goose Up
ALTER TABLE node_ping_stats ADD COLUMN sum_mbps REAL NOT NULL DEFAULT 0; -- download tasks: throughput sums over successful samples

-- +goose Down
ALTER TABLE node_ping_stats DROP COLUMN sum_mbps;
