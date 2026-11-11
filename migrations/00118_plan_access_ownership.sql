-- +goose Up
CREATE TABLE plan_access_ownership (
 plan_id INTEGER PRIMARY KEY REFERENCES plans(id) ON DELETE CASCADE,
 group_id INTEGER NOT NULL UNIQUE REFERENCES user_groups(id),
 external_group_id INTEGER REFERENCES user_groups(id)
);
-- +goose Down
-- Forward-only migration.
