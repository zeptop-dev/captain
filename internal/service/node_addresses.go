package service

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// NodeAddresses applies observed addresses independently of report processing.
// Network I/O never holds Accounts or delays billing acknowledgements. Topology
// serializes writes with manual node/ingress edits and whole-site resets.
type NodeAddresses struct {
	Store    *store.Store
	DNS      *DNS
	State    *AgentState
	Log      *slog.Logger
	Health   *DNSHealth
	wakeOnce sync.Once
	wake     chan int64
}

// Notify never blocks a report. Overflow is covered by the periodic sweep.
func (s *NodeAddresses) notifications() chan int64 {
	s.wakeOnce.Do(func() { s.wake = make(chan int64, 256) })
	return s.wake
}
func (s *NodeAddresses) Notify(id int64) {
	if s == nil {
		return
	}
	select {
	case s.notifications() <- id:
	default:
	}
}
func (s *NodeAddresses) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	s.Tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Tick(ctx, time.Now())
		case id := <-s.notifications():
			ids := map[int64]bool{id: true}
			// Bound each batch even while reports arrive; retain timer/cancellation fairness.
			for range len(s.notifications()) {
				ids[<-s.notifications()] = true
			}
			for id := range ids {
				if ctx.Err() != nil {
					return
				}
				s.reconcile(ctx, id, time.Now())
			}
		}
	}
}

func (s *NodeAddresses) Tick(ctx context.Context, at time.Time) {
	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return
		}
		s.reconcile(ctx, n.ID, at)
	}
}
func (s *NodeAddresses) reconcile(ctx context.Context, id int64, at time.Time) {
	defer s.reconcileIngressDNS(ctx, id, at)
	// Re-read pairing and observations under Topology; never trust queued state.
	// Reset/re-pair clears observations before queued work can apply them.
	refresh := false
	func() {
		s.Store.Topology.Lock()
		defer s.Store.Topology.Unlock()
		before, err := s.Store.NodeByID(ctx, id)
		if err != nil || !before.Paired {
			return
		}
		current, pending, err := s.Store.ReconcileNodeAddresses(ctx, id, at)
		if err != nil {
			return
		}
		if (current.PublicAddr != before.PublicAddr || current.V6Addr != before.V6Addr) && s.State != nil {
			s.State.Invalidate()
		}
		if !pending || s.DNS == nil {
			return
		}
		if err = s.Store.BeginAddressDNS(ctx, id, at); err != nil {
			return
		}
		dnsctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		results := s.DNS.EnsureMany(WithDNSOrigin(dnsctx, DNSOrigin{NodeID: id, Source: "node_address", Actor: "bosun"}), [2]string{current.Domain, current.PublicAddr}, [2]string{current.Domain, current.V6Addr})
		for _, r := range results {
			if r.Error != "" {
				if s.Log != nil {
					s.Log.Warn("automatic node address DNS update pending", "node", id)
				}
				return
			}
		}
		_ = s.Store.FinishAddressDNS(ctx, id)
		for _, r := range results {
			if r.Action == "created" || r.Action == "updated" || r.Action == "unchanged" {
				refresh = true
			}
		}
	}()
	// Health checks acquire Topology themselves and must run after the write lock
	// is released. The background worker, never the report handler, owns this I/O.
	if refresh && s.Health != nil {
		_, _ = s.Health.Check(ctx, id)
	}
}

// Ingress DNS has its own durable retry state because manual node/ingress edits
// and unpaired inventory can also change an inherited public endpoint.
func (s *NodeAddresses) reconcileIngressDNS(ctx context.Context, id int64, at time.Time) {
	ctx, cancelBatch := context.WithTimeout(ctx, 25*time.Second)
	defer cancelBatch()
	if s.DNS == nil {
		return
	}
	s.Store.Topology.Lock()
	defer s.Store.Topology.Unlock()
	pending, err := s.Store.PendingIngressDNS(ctx, id, at)
	if err != nil {
		return
	}
	for _, g := range pending {
		if ctx.Err() != nil {
			return
		}
		if err = s.Store.BeginIngressDNS(ctx, g.ID, at); err != nil {
			return
		}
		dnsctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		result := s.DNS.Ensure(WithDNSOrigin(dnsctx, DNSOrigin{NodeID: id, Source: "ingress:" + strconv.FormatInt(g.ID, 10), Actor: "address_follow"}), g.EntryDomain, g.EntryHost)
		cancel()
		if result.Error == "" && result.Reason == "" {
			_ = s.Store.FinishIngressDNS(ctx, g.ID)
		}
	}
}
