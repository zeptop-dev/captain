package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

var diagnosticTestAt = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func diagnosticActivate(t *testing.T, d *Diagnostics, id int64, identity, session string, at time.Time) *agentproto.DiagnosticsControl {
	t.Helper()
	c, _ := d.Control(context.Background(), id, identity, session, "nonce", "", at)
	if c == nil || c.Challenge == "" || c.RemainingMS != 0 {
		t.Fatal("new session must receive an inactive challenge")
	}
	c, _ = d.Control(context.Background(), id, identity, session, "nonce2", c.Challenge, at)
	if c == nil || c.Challenge != "" {
		t.Fatal("challenge did not activate")
	}
	return c
}
func diagnosticLease(t *testing.T, d *Diagnostics, id int64, identity, owner, lease string, principal int64, at time.Time) {
	t.Helper()
	_, err := d.Lease(context.Background(), id, identity, owner, principal, lease, false, func(context.Context) bool { return true }, at)
	if err != nil {
		t.Fatal(err)
	}
}
func diagnosticControl(d *Diagnostics, at time.Time) *agentproto.DiagnosticsControl {
	c, _ := d.Control(context.Background(), 1, "identity", "session", "nonce", "", at)
	return c
}
func diagnosticSample(c *agentproto.DiagnosticsControl, seq uint64, at time.Time) agentproto.DiagnosticsSample {
	return agentproto.DiagnosticsSample{Session: c.Session, Epoch: c.Epoch, Sequence: seq, SampledAt: at, ElapsedMS: 1000, Interfaces: []agentproto.DiagnosticsInterface{}}
}

func TestDiagnosticsSharedLeaseFreshnessAndStop(t *testing.T) {
	d := &Diagnostics{}
	ctx := context.Background()
	at := diagnosticTestAt
	diagnosticActivate(t, d, 1, "identity", "session", at)
	diagnosticLease(t, d, 1, "identity", "owner1", "viewer-0000000001", 1, at)
	c := diagnosticControl(d, at)
	if c.RemainingMS != 15000 {
		t.Fatalf("control TTL %d", c.RemainingMS)
	}
	diagnosticLease(t, d, 1, "identity", "owner2", "viewer-0000000002", 2, at)
	if next := diagnosticControl(d, at); next.Epoch != c.Epoch || next.Generation != c.Generation {
		t.Fatal("second viewer restarted shared sampler")
	}
	sample := diagnosticSample(c, 1, at)
	if err := d.Record(ctx, 1, "identity", sample, at); err != nil {
		t.Fatal(err)
	}
	v, err := d.Snapshot(ctx, 1, "identity", "owner1", "viewer-0000000001", 0, at.Add(time.Second))
	if err != nil || v.Status != "live" || v.FreshForMS != 2000 || len(v.Samples) != 1 || v.Samples[0].NetUpBytesPerSecond != nil {
		t.Fatalf("bad live snapshot %+v %v", v, err)
	}
	v, _ = d.Snapshot(ctx, 1, "identity", "owner1", "viewer-0000000001", 0, at.Add(4*time.Second))
	if v.Status != "stale" || v.FreshForMS != 0 {
		t.Fatal("old sample remained green")
	}
	if err = d.Release(1, "identity", "owner1", "viewer-0000000001", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if next := diagnosticControl(d, at.Add(time.Second)); next.Epoch != c.Epoch || next.RemainingMS == 0 {
		t.Fatal("one viewer stopped another")
	}
	if err = d.Release(1, "identity", "owner2", "viewer-0000000002", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if next := diagnosticControl(d, at.Add(2*time.Second)); next.RemainingMS != 0 || next.Epoch == c.Epoch {
		t.Fatal("last release did not stop and retire samples")
	}
}

func TestDiagnosticsChallengeCannotDisplaceActiveOrResurrectOldSession(t *testing.T) {
	d := &Diagnostics{}
	ctx := context.Background()
	at := diagnosticTestAt
	diagnosticActivate(t, d, 1, "identity", "session", at)
	diagnosticLease(t, d, 1, "identity", "owner", "viewer-0000000001", 1, at)
	active := diagnosticControl(d, at)
	if err := d.Record(ctx, 1, "identity", diagnosticSample(active, 1, at), at); err != nil {
		t.Fatal(err)
	}
	b, _ := d.Control(ctx, 1, "identity", "b", "nonce", "", at.Add(time.Second))
	c, _ := d.Control(ctx, 1, "identity", "c", "nonce", "", at.Add(time.Second))
	if got := diagnosticControl(d, at.Add(time.Second)); got.Epoch != active.Epoch {
		t.Fatal("unknown first GET displaced live epoch")
	}
	b, _ = d.Control(ctx, 1, "identity", "b", "nonce", b.Challenge, at.Add(2*time.Second))
	if b.RemainingMS == 0 || b.Epoch == active.Epoch {
		t.Fatal("verified replacement failed")
	}
	late, _ := d.Control(ctx, 1, "identity", "c", "nonce", c.Challenge, at.Add(3*time.Second))
	if late.RemainingMS != 0 || late.Challenge == "" {
		t.Fatal("old-parent challenge activated")
	}
	if err := d.Record(ctx, 1, "identity", diagnosticSample(active, 2, at.Add(3*time.Second)), at.Add(3*time.Second)); !errors.Is(err, ErrDiagnosticsLease) {
		t.Fatal("old epoch accepted")
	}
	if old := diagnosticControl(d, at.Add(3*time.Second)); old.RemainingMS != 0 {
		t.Fatal("retired session reactivated")
	}
	v, _ := d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000001", 0, at.Add(3*time.Second))
	if len(v.Samples) != 0 || v.Epoch != b.Epoch {
		t.Fatal("new process mixed old samples")
	}
}

func TestDiagnosticsAuthorizationExpiryIdentityAndDeleteBeforeCreate(t *testing.T) {
	d := &Diagnostics{}
	ctx := context.Background()
	at := diagnosticTestAt
	diagnosticActivate(t, d, 1, "identity", "session", at)
	var allowed atomic.Bool
	allowed.Store(true)
	_, err := d.Lease(ctx, 1, "identity", "owner", 1, "viewer-0000000001", false, func(context.Context) bool { return allowed.Load() }, at)
	if err != nil {
		t.Fatal(err)
	}
	c := diagnosticControl(d, at)
	allowed.Store(false)
	if err = d.Record(ctx, 1, "identity", diagnosticSample(c, 1, at), at); !errors.Is(err, ErrDiagnosticsLease) {
		t.Fatal("revoked viewer still sampled")
	}
	if c = diagnosticControl(d, at); c.RemainingMS != 0 {
		t.Fatal("revocation did not stop")
	}
	if err = d.Release(1, "identity", "owner", "viewer-late-00001", at); err != nil {
		t.Fatal(err)
	}
	_, err = d.Lease(ctx, 1, "identity", "owner", 1, "viewer-late-00001", false, func(context.Context) bool { return true }, at.Add(time.Second))
	if !errors.Is(err, ErrDiagnosticsLease) {
		t.Fatal("late create resurrected released lease")
	}
	diagnosticLease(t, d, 1, "identity", "owner", "viewer-0000000002", 1, at)
	if c = diagnosticControl(d, at.Add(31*time.Second)); c.RemainingMS != 0 {
		t.Fatal("unrenewed viewer did not expire")
	}
	diagnosticActivate(t, d, 1, "new-token", "new-session", at.Add(32*time.Second))
	v, _ := d.Snapshot(ctx, 1, "new-token", "owner", "viewer-0000000002", 0, at.Add(32*time.Second))
	if v.Status != "expired" || len(v.Samples) > 0 {
		t.Fatal("re-pair reused viewer or samples")
	}
	d.Reset()
	if len(d.nodes) != 0 || len(d.tombstones) != 0 {
		t.Fatal("reset retained identity state")
	}
}

func TestDiagnosticsLimitsRingAndSampleValidation(t *testing.T) {
	d := &Diagnostics{}
	ctx := context.Background()
	at := diagnosticTestAt
	diagnosticActivate(t, d, 1, "identity", "session", at)
	for i := 0; i < 4; i++ {
		diagnosticLease(t, d, 1, "identity", "owner", fmt.Sprintf("viewer-%016d", i), 1, at)
	}
	if _, err := d.Lease(ctx, 1, "identity", "owner", 1, "viewer-overlimit", false, func(context.Context) bool { return true }, at); !errors.Is(err, ErrDiagnosticsLimit) {
		t.Fatal("principal budget missing")
	}
	for i := 1; i <= 121; i++ {
		when := at.Add(time.Duration(i) * time.Second)
		if i%10 == 0 {
			if _, err := d.Lease(ctx, 1, "identity", "owner", 1, "viewer-0000000000000000", true, func(context.Context) bool { return true }, when); err != nil {
				t.Fatal(err)
			}
		}
		c := diagnosticControl(d, when)
		if err := d.Record(ctx, 1, "identity", diagnosticSample(c, uint64(i), when), when); err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
	}
	when := at.Add(121 * time.Second)
	v, _ := d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000000000000", 0, when)
	if len(v.Samples) != 120 || v.Samples[0].Sequence != 2 || v.LatestSequence != 121 {
		t.Fatalf("unbounded ring: %+v", v)
	}
	c := diagnosticControl(d, when)
	for _, mutate := range []func(*agentproto.DiagnosticsSample){
		func(s *agentproto.DiagnosticsSample) { s.Sequence = 9007199254740992 },
		func(s *agentproto.DiagnosticsSample) { s.SampledAt = when.Add(-time.Minute) },
		func(s *agentproto.DiagnosticsSample) { s.Interfaces = make([]agentproto.DiagnosticsInterface, 17) },
		func(s *agentproto.DiagnosticsSample) {
			s.Interfaces = []agentproto.DiagnosticsInterface{{Name: "secret\nline"}}
		},
	} {
		s := diagnosticSample(c, 122, when.Add(time.Second))
		mutate(&s)
		if err := d.Record(ctx, 1, "identity", s, when.Add(time.Second)); !errors.Is(err, ErrDiagnosticsSample) {
			t.Fatal("invalid sample accepted", err)
		}
	}
}

func TestDiagnosticsConcurrentViewersAndSnapshots(t *testing.T) {
	d := &Diagnostics{}
	at := time.Now()
	diagnosticActivate(t, d, 1, "identity", "session", at)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			owner := fmt.Sprint(i)
			lease := fmt.Sprintf("viewer-%016d", i)
			for j := 0; j < 20; j++ {
				_, _ = d.Lease(context.Background(), 1, "identity", owner, int64(i), lease, false, func(context.Context) bool { return true }, time.Now())
				_, _ = d.Snapshot(context.Background(), 1, "identity", owner, lease, 0, time.Now())
				_ = d.Release(1, "identity", owner, lease, time.Now())
			}
		})
	}
	wg.Wait()
}

func TestDiagnosticsNodeAndGlobalBudgets(t *testing.T) {
	at := diagnosticTestAt
	ctx := context.Background()
	for _, tc := range []struct {
		name           string
		nodes, perNode int
		target         int64
	}{
		{"per-node", 1, 8, 1}, {"active-nodes", 8, 1, 9}, {"total-viewers", 4, 8, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Diagnostics{}
			for node := 1; node <= tc.nodes; node++ {
				diagnosticActivate(t, d, int64(node), "identity", "session", at)
				for v := 0; v < tc.perNode; v++ {
					principal := int64(node*10 + v)
					diagnosticLease(t, d, int64(node), "identity", fmt.Sprint(principal), fmt.Sprintf("viewer-%016d", principal), principal, at)
				}
			}
			if tc.target > int64(tc.nodes) {
				diagnosticActivate(t, d, tc.target, "identity", "session", at)
			}
			if _, err := d.Lease(ctx, tc.target, "identity", "extra", 999, "viewer-overflow1", false, func(context.Context) bool { return true }, at); !errors.Is(err, ErrDiagnosticsLimit) {
				t.Fatal("budget missing", err)
			}
		})
	}
}

func TestDiagnosticsDelayedSamplesCannotRefreshOrBypassRateBudget(t *testing.T) {
	d := &Diagnostics{}
	at := diagnosticTestAt
	ctx := context.Background()
	diagnosticActivate(t, d, 1, "identity", "session", at)
	diagnosticLease(t, d, 1, "identity", "owner", "viewer-0000000001", 1, at)
	c := diagnosticControl(d, at)
	s := diagnosticSample(c, 1, at.Add(-4*time.Second))
	if err := d.Record(ctx, 1, "identity", s, at); err != nil {
		t.Fatal(err)
	}
	v, _ := d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000001", 0, at)
	if v.Status != "stale" || v.FreshForMS != 0 {
		t.Fatal("late sample became fresh")
	}
	s.Sequence++
	s.SampledAt = s.SampledAt.Add(time.Millisecond)
	if err := d.Record(ctx, 1, "identity", s, at.Add(time.Millisecond)); !errors.Is(err, ErrDiagnosticsLimit) {
		t.Fatal("old sample timestamp bypassed ingress budget", err)
	}
	// Mutating a caller-owned sample or returned view must not mutate the ring.
	rate := float64(7)
	s = diagnosticSample(c, 2, at.Add(time.Second))
	s.NetUpBytesPerSecond = &rate
	if err := d.Record(ctx, 1, "identity", s, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rate = 999
	v, _ = d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000001", 1, at.Add(time.Second))
	if *v.Samples[0].NetUpBytesPerSecond != 7 {
		t.Fatal("record retained caller pointer")
	}
	*v.Samples[0].NetUpBytesPerSecond = 888
	v, _ = d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000001", 1, at.Add(time.Second))
	if *v.Samples[0].NetUpBytesPerSecond != 7 {
		t.Fatal("view retained broker pointer")
	}
}

func TestDiagnosticsCancelledReadDoesNotRevokeOtherViewers(t *testing.T) {
	d := &Diagnostics{}
	at := diagnosticTestAt
	diagnosticActivate(t, d, 1, "identity", "session", at)
	diagnosticLease(t, d, 1, "identity", "owner", "viewer-0000000001", 1, at)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = d.Snapshot(ctx, 1, "identity", "owner", "viewer-0000000001", 0, at)
	v, err := d.Snapshot(context.Background(), 1, "identity", "owner", "viewer-0000000001", 0, at)
	if err != nil || v.Status == "expired" {
		t.Fatal("cancelled request revoked a valid viewer")
	}
}
