-- +goose Up
-- Raster assets live with the settings they support, so database backups and
-- an atomic site reset include them without filesystem coordination.
CREATE TABLE brand_assets (
    digest     TEXT PRIMARY KEY CHECK(length(digest) = 64),
    png        BLOB NOT NULL CHECK(length(png) > 0 AND length(png) <= 4194304),
    created_at INTEGER NOT NULL
);
