package agenthttps

import (
	"context"
	"sync"
)

// Admission and closure share a lock, so Wait never races an Add at zero.
// HTTP/2 connection drain timers are deliberately not request-completion proof.
type requestGate struct {
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (g *requestGate) begin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.active.Add(1)
	return true
}
func (g *requestGate) done()  { g.active.Done() }
func (g *requestGate) close() { g.mu.Lock(); g.closed = true; g.mu.Unlock() }
func (g *requestGate) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { g.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
