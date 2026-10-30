package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

const (
	DiagnosticsViewerTTL       = 30 * time.Second
	diagnosticsFresh           = 3 * time.Second
	diagnosticsCapabilityFresh = 70 * time.Second
	diagnosticsControlTTL      = 15 * time.Second
	diagnosticsRefresh         = 5 * time.Second
)

var (
	ErrDiagnosticsUnavailable = errors.New("diagnostics_unavailable")
	ErrDiagnosticsLimit       = errors.New("diagnostics_budget_exceeded")
	ErrDiagnosticsLease       = errors.New("diagnostics_lease_not_found")
	ErrDiagnosticsSample      = errors.New("invalid_diagnostics_sample")
	DiagnosticsIDPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,64}$`)
)

type DiagnosticsLease struct {
	LeaseID      string `json:"lease_id"`
	TTLMS        int64  `json:"ttl_ms"`
	RenewAfterMS int64  `json:"renew_after_ms"`
	IntervalMS   int    `json:"interval_ms"`
}
type DiagnosticsSnapshot struct {
	Status         string                         `json:"status"`
	IntervalMS     int                            `json:"interval_ms"`
	FreshForMS     int64                          `json:"fresh_for_ms"`
	NodeID         int64                          `json:"node_id"`
	Epoch          string                         `json:"epoch"`
	LatestSequence uint64                         `json:"latest_sequence"`
	Samples        []agentproto.DiagnosticsSample `json:"samples"`
}

type diagnosticViewer struct {
	id, owner string
	principal int64
	until     time.Time
	valid     func(context.Context) bool
}
type diagnosticCandidate struct {
	challenge, parent, epoch string
	generation               uint64
	until                    time.Time
}
type diagnosticNode struct {
	identity                                                   string
	session, head, epoch                                       string
	generation                                                 uint64
	commandAt, capabilityAt, acceptedAt, freshUntil, touchedAt time.Time
	running                                                    bool
	wake                                                       chan struct{}
	viewers                                                    map[string]*diagnosticViewer
	candidates                                                 map[string]diagnosticCandidate
	retired                                                    map[string]time.Time
	samples                                                    []agentproto.DiagnosticsSample
}
type diagnosticTombstone struct{ until time.Time }

// Diagnostics is deliberately independent of Probe and AgentState: it has no
// database writer, history aggregation, billing, alarms or configuration cache.
// All memory is bounded and expires. Authorization callbacks only do local
// reads and are called while the request holds the short Accounts read lock.
type Diagnostics struct {
	mu         sync.Mutex
	nodes      map[int64]*diagnosticNode
	tombstones map[string]diagnosticTombstone
}

func diagID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func diagCursor(epoch string, generation uint64) string {
	return epoch + ":" + strconv.FormatUint(generation, 10)
}
func diagWake(n *diagnosticNode) { close(n.wake); n.wake = make(chan struct{}) }
func (d *Diagnostics) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range d.nodes {
		diagWake(n)
	}
	d.nodes = nil
	d.tombstones = nil
}
func (d *Diagnostics) prune(at time.Time) {
	for k, t := range d.tombstones {
		if !at.Before(t.until) {
			delete(d.tombstones, k)
		}
	}
	for id, n := range d.nodes {
		for k, v := range n.viewers {
			if !at.Before(v.until) {
				delete(n.viewers, k)
			}
		}
		for k, c := range n.candidates {
			if !at.Before(c.until) {
				delete(n.candidates, k)
			}
		}
		for k, t := range n.retired {
			if !at.Before(t) {
				delete(n.retired, k)
			}
		}
		if len(n.viewers) == 0 && at.Sub(n.touchedAt) > 5*time.Minute {
			diagWake(n)
			delete(d.nodes, id)
		}
	}
}
func (d *Diagnostics) node(id int64, identity string, at time.Time) *diagnosticNode {
	if d.nodes == nil {
		d.nodes = make(map[int64]*diagnosticNode)
	}
	n := d.nodes[id]
	if n != nil && n.identity != identity {
		diagWake(n)
		delete(d.nodes, id)
		n = nil
	}
	if n == nil {
		// Capability-only state is bounded even for a large installation.
		if len(d.nodes) >= 1024 {
			return nil
		}
		n = &diagnosticNode{identity: identity, head: diagID(), epoch: diagID(), wake: make(chan struct{}), viewers: map[string]*diagnosticViewer{}, candidates: map[string]diagnosticCandidate{}, retired: map[string]time.Time{}}
		d.nodes[id] = n
	}
	n.touchedAt = at
	return n
}
func (d *Diagnostics) validViewers(ctx context.Context, n *diagnosticNode, at time.Time) time.Time {
	var latest time.Time
	for k, v := range n.viewers {
		if !at.Before(v.until) || v.valid == nil {
			delete(n.viewers, k)
			continue
		}
		// Cancelling one browser fetch or agent long poll is not evidence that
		// every other viewer has lost authorization. A cancelled local read
		// leaves leases bounded by their existing deadlines; it never renews.
		if ctx.Err() == nil && !v.valid(ctx) && ctx.Err() == nil {
			delete(n.viewers, k)
			continue
		}
		if v.until.After(latest) {
			latest = v.until
		}
	}
	return latest
}
func (d *Diagnostics) updateRunning(n *diagnosticNode, at, until time.Time) {
	running := until.After(at) && n.session != ""
	if running != n.running {
		n.running = running
		n.generation++
		n.commandAt = at
		n.epoch = diagID()
		n.samples = nil
		n.acceptedAt = time.Time{}
		n.freshUntil = time.Time{}
		diagWake(n)
	} else if running && at.Sub(n.commandAt) >= diagnosticsRefresh {
		n.generation++
		n.commandAt = at
		diagWake(n)
	}
}

// Control is invoked only after re-resolving the agent token. An unknown
// process receives a challenge bound to the current head; its first delayed
// request cannot evict a live process, and an old echoed challenge cannot
// reactivate after another process takes over.
func (d *Diagnostics) Control(ctx context.Context, id int64, identity, session, nonce, echo string, at time.Time) (*agentproto.DiagnosticsControl, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(at)
	n := d.node(id, identity, at)
	if n == nil {
		return nil, nil
	}
	if _, retired := n.retired[session]; retired {
		return &agentproto.DiagnosticsControl{Session: session, RequestNonce: nonce, Epoch: n.epoch, Generation: n.generation, IntervalMS: 1000}, n.wake
	}
	if n.session != session {
		c, ok := n.candidates[session]
		if ok && echo == c.challenge && c.parent == n.head && at.Before(c.until) {
			if n.session != "" {
				n.retired[n.session] = at.Add(10 * time.Minute)
			}
			n.session = session
			n.head = diagID()
			n.epoch = diagID()
			n.generation++
			n.commandAt = at
			n.samples = nil
			n.acceptedAt = time.Time{}
			n.freshUntil = time.Time{}
			delete(n.candidates, session)
			diagWake(n)
		} else {
			if !ok || c.parent != n.head {
				if len(n.candidates) >= 8 || len(n.retired) >= 64 {
					return nil, n.wake
				}
				n.generation++
				c = diagnosticCandidate{challenge: diagID(), parent: n.head, epoch: diagID(), generation: n.generation, until: at.Add(time.Minute)}
				n.candidates[session] = c
			}
			return &agentproto.DiagnosticsControl{Session: session, RequestNonce: nonce, Challenge: c.challenge, Epoch: c.epoch, Generation: c.generation, IntervalMS: 1000}, n.wake
		}
	}
	n.capabilityAt = at
	until := d.validViewers(ctx, n, at)
	d.updateRunning(n, at, until)
	remaining := time.Duration(0)
	if n.running {
		remaining = until.Sub(at)
		if remaining > diagnosticsControlTTL {
			remaining = diagnosticsControlTTL
		}
	}
	return &agentproto.DiagnosticsControl{Session: session, RequestNonce: nonce, Epoch: n.epoch, Generation: n.generation, RemainingMS: remaining.Milliseconds(), IntervalMS: 1000}, n.wake
}

func diagTombstoneKey(id int64, owner, lease string) string {
	return strconv.FormatInt(id, 10) + ":" + owner + ":" + lease
}
func (d *Diagnostics) Lease(ctx context.Context, id int64, identity, owner string, principal int64, lease string, renew bool, valid func(context.Context) bool, at time.Time) (DiagnosticsLease, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(at)
	if !DiagnosticsIDPattern.MatchString(lease) {
		return DiagnosticsLease{}, ErrDiagnosticsLease
	}
	n := d.node(id, identity, at)
	if n == nil || n.session == "" || at.Sub(n.capabilityAt) > diagnosticsCapabilityFresh {
		return DiagnosticsLease{}, ErrDiagnosticsUnavailable
	}
	if _, dead := d.tombstones[diagTombstoneKey(id, owner, lease)]; dead {
		return DiagnosticsLease{}, ErrDiagnosticsLease
	}
	v := n.viewers[lease]
	if v != nil && v.owner != owner {
		return DiagnosticsLease{}, ErrDiagnosticsLease
	}
	if renew && v == nil {
		return DiagnosticsLease{}, ErrDiagnosticsLease
	}
	if v == nil {
		if len(d.tombstones) >= 480 {
			return DiagnosticsLease{}, ErrDiagnosticsLimit
		}
		total, own, activeNodes := 0, 0, 0
		for _, other := range d.nodes {
			if len(other.viewers) > 0 {
				activeNodes++
			}
			for _, viewer := range other.viewers {
				total++
				if viewer.principal == principal {
					own++
				}
			}
		}
		if len(n.viewers) >= 8 || total >= 32 || own >= 4 || (len(n.viewers) == 0 && activeNodes >= 8) {
			return DiagnosticsLease{}, ErrDiagnosticsLimit
		}
		v = &diagnosticViewer{id: lease, owner: owner, principal: principal}
		n.viewers[lease] = v
	}
	if v.until.Sub(at) > DiagnosticsViewerTTL-diagnosticsRefresh {
		return DiagnosticsLease{LeaseID: lease, TTLMS: v.until.Sub(at).Milliseconds(), RenewAfterMS: 10000, IntervalMS: 1000}, nil
	}
	v.until = at.Add(DiagnosticsViewerTTL)
	v.valid = valid
	// The timer only removes this exact lease generation. A renewal cannot be
	// cancelled by its predecessor's timer, and expiry wakes long-poll waiters.
	expires := v.until
	time.AfterFunc(DiagnosticsViewerTTL, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		current := d.nodes[id]
		if current != n {
			return
		}
		if live := n.viewers[lease]; live == v && live.until.Equal(expires) {
			delete(n.viewers, lease)
			if len(n.viewers) == 0 {
				d.updateRunning(n, time.Now(), time.Time{})
			}
			diagWake(n)
		}
	})
	d.updateRunning(n, at, d.validViewers(ctx, n, at))
	diagWake(n)
	return DiagnosticsLease{LeaseID: lease, TTLMS: 30000, RenewAfterMS: 10000, IntervalMS: 1000}, nil
}
func (d *Diagnostics) Release(id int64, identity, owner, lease string, at time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(at)
	if !DiagnosticsIDPattern.MatchString(lease) {
		return ErrDiagnosticsLease
	}
	if d.tombstones == nil {
		d.tombstones = map[string]diagnosticTombstone{}
	}
	k := diagTombstoneKey(id, owner, lease)
	owned := false
	if n := d.nodes[id]; n != nil && n.identity == identity {
		owned = n.viewers[lease] != nil && n.viewers[lease].owner == owner
	}
	// Reserve 32 slots for the bounded active leases, so a flood of unknown
	// DELETEs cannot prevent an existing viewer from releasing immediately.
	if _, exists := d.tombstones[k]; !exists && len(d.tombstones) >= 480 && !owned {
		return ErrDiagnosticsLimit
	}
	d.tombstones[k] = diagnosticTombstone{until: at.Add(5 * time.Minute)}
	if n := d.nodes[id]; n != nil && n.identity == identity {
		if v := n.viewers[lease]; v != nil && v.owner == owner {
			delete(n.viewers, lease)
			if len(n.viewers) == 0 {
				d.updateRunning(n, at, time.Time{})
			}
			diagWake(n)
		}
	}
	return nil
}
func (d *Diagnostics) Snapshot(ctx context.Context, id int64, identity, owner, lease string, after uint64, at time.Time) (DiagnosticsSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(at)
	out := DiagnosticsSnapshot{Status: "expired", NodeID: id, IntervalMS: 1000, Samples: []agentproto.DiagnosticsSample{}}
	n := d.nodes[id]
	if n == nil || n.identity != identity {
		return out, nil
	}
	until := d.validViewers(ctx, n, at)
	d.updateRunning(n, at, until)
	v := n.viewers[lease]
	if v == nil {
		return out, nil
	}
	if v.owner != owner {
		return out, ErrDiagnosticsLease
	}
	out.Epoch = n.epoch
	out.Status = "waiting"
	if at.Sub(n.capabilityAt) > diagnosticsCapabilityFresh {
		out.Status = "stale"
	}
	if len(n.samples) > 0 {
		out.LatestSequence = n.samples[len(n.samples)-1].Sequence
		fresh := n.freshUntil.Sub(at)
		if fresh > diagnosticsFresh {
			fresh = 0
		}
		out.Status = "stale"
		if fresh > 0 {
			out.Status = "live"
			out.FreshForMS = fresh.Milliseconds()
		}
		for _, s := range n.samples {
			if s.Sequence > after {
				out.Samples = append(out.Samples, cloneDiagnosticSample(s))
			}
		}
	}
	return out, nil
}
func cloneDiagnosticSample(s agentproto.DiagnosticsSample) agentproto.DiagnosticsSample {
	s.CPUPercent = cloneDiagnosticNumber(s.CPUPercent)
	s.MemoryUsed = cloneDiagnosticNumber(s.MemoryUsed)
	s.MemoryTotal = cloneDiagnosticNumber(s.MemoryTotal)
	s.NetUpBytesPerSecond = cloneDiagnosticNumber(s.NetUpBytesPerSecond)
	s.NetDownBytesPerSecond = cloneDiagnosticNumber(s.NetDownBytesPerSecond)
	s.Interfaces = append([]agentproto.DiagnosticsInterface(nil), s.Interfaces...)
	for i := range s.Interfaces {
		s.Interfaces[i].RXBytesPerSecond = cloneDiagnosticNumber(s.Interfaces[i].RXBytesPerSecond)
		s.Interfaces[i].TXBytesPerSecond = cloneDiagnosticNumber(s.Interfaces[i].TXBytesPerSecond)
	}
	return s
}
func cloneDiagnosticNumber[T any](v *T) *T {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func validDiagnosticNumber(x *float64, max float64) bool {
	return x == nil || (!math.IsNaN(*x) && !math.IsInf(*x, 0) && *x >= 0 && *x <= max)
}
func validDiagnosticSample(s agentproto.DiagnosticsSample, at time.Time) bool {
	if s.Sequence == 0 || s.Sequence > 9007199254740991 || s.ElapsedMS < 0 || s.ElapsedMS > 9007199254740991 || s.SampledAt.Before(at.Add(-10*time.Second)) || s.SampledAt.After(at.Add(3*time.Second)) || len(s.Interfaces) > 16 || !validDiagnosticNumber(s.CPUPercent, 100) || !validDiagnosticNumber(s.NetUpBytesPerSecond, 1e15) || !validDiagnosticNumber(s.NetDownBytesPerSecond, 1e15) {
		return false
	}
	if s.MemoryUsed != nil && s.MemoryTotal != nil && *s.MemoryUsed > *s.MemoryTotal {
		return false
	}
	names := map[string]bool{}
	for _, nic := range s.Interfaces {
		if len(nic.Name) == 0 || len(nic.Name) > 64 || names[nic.Name] || !validDiagnosticNumber(nic.RXBytesPerSecond, 1e15) || !validDiagnosticNumber(nic.TXBytesPerSecond, 1e15) {
			return false
		}
		for _, r := range nic.Name {
			if r < 32 || r == 127 {
				return false
			}
		}
		names[nic.Name] = true
	}
	return true
}
func (d *Diagnostics) Record(ctx context.Context, id int64, identity string, s agentproto.DiagnosticsSample, at time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(at)
	n := d.nodes[id]
	if n == nil || n.identity != identity {
		return ErrDiagnosticsLease
	}
	d.updateRunning(n, at, d.validViewers(ctx, n, at))
	if !n.running || n.session != s.Session || n.epoch != s.Epoch {
		return ErrDiagnosticsLease
	}
	if !validDiagnosticSample(s, at) {
		return ErrDiagnosticsSample
	}
	if len(n.samples) > 0 {
		last := n.samples[len(n.samples)-1]
		if s.Sequence <= last.Sequence || !s.SampledAt.After(last.SampledAt) {
			return ErrDiagnosticsSample
		}
		if at.Sub(n.acceptedAt) < 500*time.Millisecond {
			return ErrDiagnosticsLimit
		}
	}
	// A delayed sample cannot obtain a fresh three seconds merely by arriving.
	n.acceptedAt = at
	age := at.Sub(s.SampledAt)
	if age < 0 {
		age = 0
	}
	n.freshUntil = at.Add(diagnosticsFresh - age)
	n.samples = append(n.samples, cloneDiagnosticSample(s))
	if len(n.samples) > 120 {
		n.samples = append([]agentproto.DiagnosticsSample(nil), n.samples[len(n.samples)-120:]...)
	}
	return nil
}
