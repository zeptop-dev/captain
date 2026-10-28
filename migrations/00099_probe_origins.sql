-- +goose Up
-- Once a hostname has served third-party JavaScript it must never fall through
-- to Captain's privileged application, even after the theme/settings are reset.
-- Browser caches and service workers can outlive the server-side configuration.
CREATE TABLE probe_origins (
    hostname TEXT PRIMARY KEY CHECK(length(hostname) BETWEEN 1 AND 253)
);
