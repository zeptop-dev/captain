package store

import (
	"context"
	"time"
)

// ProbeNodeRecord contains persisted node identity and row timestamps. The
// update time can advance on reports as well as metadata edits; it is not a
// substitute for the independently accepted metric receipt time.
type ProbeNodeRecord struct {
	UUID      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProbePublicRecords is a private lookup. Consumers must still apply public
// pairing, visibility and category policy before exposing a node.
func (s *Store) ProbePublicRecords(ctx context.Context) (map[int64]ProbeNodeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.node_id,p.uuid,n.created_at,n.updated_at FROM probe_public_ids p JOIN nodes n ON n.id=p.node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ProbeNodeRecord{}
	for rows.Next() {
		var id, created, updated int64
		var record ProbeNodeRecord
		if err := rows.Scan(&id, &record.UUID, &created, &updated); err != nil {
			return nil, err
		}
		record.CreatedAt, record.UpdatedAt = unix(created), unix(updated)
		out[id] = record
	}
	return out, rows.Err()
}

// ProbePublicIDs maps internal row IDs to persisted, per-incarnation UUIDs.
// This is a private lookup; callers must apply the public node projection.
func (s *Store) ProbePublicIDs(ctx context.Context) (map[int64]string, error) {
	records, err := s.ProbePublicRecords(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for id, record := range records {
		out[id] = record.UUID
	}
	return out, nil
}
