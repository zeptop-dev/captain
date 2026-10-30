package store

import "context"

// NodeDiagnosticsIdentity is a private memory-cache key. It is never returned
// by an HTTP DTO. A new pairing (including a reused node ID after reset) cannot
// inherit an old process, viewer lease, or sample ring.
func (s *Store) NodeDiagnosticsIdentity(ctx context.Context, id int64) (string, error) {
	var identity string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(token_hash,'') FROM nodes WHERE id=?`, id).Scan(&identity)
	return identity, wrapNotFound(err)
}
