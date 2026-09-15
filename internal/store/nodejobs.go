package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// NodeJob is a one-off task handed to a node through its state; the result
// comes back inside the node's report.
type NodeJob struct {
	ID        string          `json:"id"`
	NodeID    int64           `json:"node_id"`
	Kind      string          `json:"kind"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	DoneAt    *time.Time      `json:"done_at,omitempty"`
}

// CreateNodeJob queues a job and forgets finished jobs older than an hour.
func (s *Store) CreateNodeJob(ctx context.Context, id string, nodeID int64, kind string, params json.RawMessage) error {
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM node_jobs WHERE node_id = ? AND ((done_at IS NOT NULL AND done_at < ?) OR created_at < ?)`, nodeID, time.Now().Add(-time.Hour).Unix(), time.Now().Add(-6*time.Hour).Unix())
	_, err := s.db.ExecContext(ctx, `INSERT INTO node_jobs (id, node_id, kind, params_json, created_at) VALUES (?, ?, ?, ?, ?)`, id, nodeID, kind, string(params), time.Now().Unix())
	return err
}

// PendingNodeJobs lists jobs the node has not answered yet.
func (s *Store) PendingNodeJobs(ctx context.Context, nodeID int64) ([]NodeJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, node_id, kind, params_json, result_json, error, created_at, done_at FROM node_jobs WHERE node_id = ? AND done_at IS NULL ORDER BY created_at`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeJob
	for rows.Next() {
		j, err := scanNodeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// NodeJob returns one job of a node.
func (s *Store) NodeJob(ctx context.Context, nodeID int64, id string) (*NodeJob, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, node_id, kind, params_json, result_json, error, created_at, done_at FROM node_jobs WHERE node_id = ? AND id = ?`, nodeID, id)
	j, err := scanNodeJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// CompleteNodeJob stores a node's answer.
func (s *Store) CompleteNodeJob(ctx context.Context, nodeID int64, id string, result json.RawMessage, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE node_jobs SET result_json = ?, error = ?, done_at = ? WHERE node_id = ? AND id = ? AND done_at IS NULL`, string(result), errText, time.Now().Unix(), nodeID, id)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanNodeJob(r rowScanner) (NodeJob, error) {
	var j NodeJob
	var params, result string
	var created int64
	var done sql.NullInt64
	if err := r.Scan(&j.ID, &j.NodeID, &j.Kind, &params, &result, &j.Error, &created, &done); err != nil {
		return j, err
	}
	j.Params = json.RawMessage(params)
	if result != "" {
		j.Result = json.RawMessage(result)
	}
	j.CreatedAt = time.Unix(created, 0)
	if done.Valid {
		t := time.Unix(done.Int64, 0)
		j.DoneAt = &t
	}
	return j, nil
}
