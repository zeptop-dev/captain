package store

import (
	"context"
	"time"
)

// AcquireMonitorRead serializes decode-heavy admin history queries separately
// from SQLite ownership. Waiting requests do not hold the sole DB connection.
func (s *Store) AcquireMonitorRead(ctx context.Context) (context.Context, func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	select {
	case s.monitorReads <- struct{}{}:
		return ctx, func() { <-s.monitorReads; cancel() }, nil
	case <-ctx.Done():
		cancel()
		return nil, nil, ctx.Err()
	}
}
