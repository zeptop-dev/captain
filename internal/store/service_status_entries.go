package store

import (
	"context"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
)

// ServiceStatusEntries selects eligible subscription entries in one query.
// An empty restricted set never scans private entries. Nil with restricted=false
// is reserved for the administrator preview, not anonymous publication.
func (s *Store) ServiceStatusEntries(ctx context.Context, ids []int64, restricted bool) ([]*domain.Entry, error) {
	out := []*domain.Entry{}
	if restricted && len(ids) == 0 {
		return out, nil
	}
	q := `SELECT ` + entryCols + ` FROM entries e JOIN inbounds i ON i.id=e.inbound_id WHERE e.enabled=1 AND i.enabled=1`
	var args []any
	if restricted {
		marks := make([]string, len(ids))
		for i, id := range ids {
			marks[i] = "?"
			args = append(args, id)
		}
		q += ` AND e.id IN (` + strings.Join(marks, ",") + `)`
	}
	q += ` ORDER BY e.sort,e.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}
