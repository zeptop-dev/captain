package store

import "context"

// ProbePublicIDs maps internal row IDs to persisted, per-incarnation UUIDs.
// This is a private lookup; callers must apply the public node projection.
func (s *Store) ProbePublicIDs(ctx context.Context) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, uuid FROM probe_public_ids`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var uuid string
		if err := rows.Scan(&id, &uuid); err != nil {
			return nil, err
		}
		out[id] = uuid
	}
	return out, rows.Err()
}
