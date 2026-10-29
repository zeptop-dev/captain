package http

import (
	"context"
	"time"
)

// RunLineRollouts advances only durable forwarding plans. It does not perform
// network I/O or retain user credentials; fresh user state still uses Build.
func (s *Server) RunLineRollouts(ctx context.Context) {
	tick := func() {
		err := s.advanceLineRollouts(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Error("line release scheduling failed", "err", err)
		}
	}
	tick()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func (s *Server) advanceLineRollouts(ctx context.Context) error {
	s.store.Accounts.RLock()
	defer s.store.Accounts.RUnlock()
	s.store.Topology.Lock()
	defer s.store.Topology.Unlock()
	nodes, err := s.store.AdvanceLineRollouts(ctx, time.Now(), 20)
	if len(nodes) > 0 {
		s.state.Invalidate()
	}
	return err
}
