-- +goose Up
CREATE TABLE node_jobs (
  id TEXT PRIMARY KEY,
  node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  params_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  done_at INTEGER
);
CREATE INDEX node_jobs_node ON node_jobs(node_id, done_at);

-- +goose Down
DROP TABLE node_jobs;
