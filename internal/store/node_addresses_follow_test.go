package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func fallbackFollowSample(at time.Time, address string, v6 bool) *agentproto.PublicAddresses {
	f := &agentproto.PublicAddressFallback{Address: address, Source: agentproto.PublicAddressFallbackSource, Origin: "https://panel.example.com:2096", SampledAt: at.Unix(), CheckStatus: agentproto.PublicAddressFallbackCheckNoConflict, RunID: strings.Repeat("a", 32)}
	out := &agentproto.PublicAddresses{SampledAt: at.Unix(), FallbackIPv4: f}
	if v6 {
		out.FallbackIPv4 = nil
		out.FallbackIPv6 = f
	}
	return out
}

func newFallbackFollowStore(t *testing.T) (*Store, *domain.Node, time.Time) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "follow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "restricted", PublicAddr: "198.51.100.20", V6Addr: "2001:db8::20"}
	if err = s.CreateNodeWithAddressPolicy(ctx, n, "pair", time.Hour, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemPairCode(ctx, "pair", "token", "node", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	return s, n, time.Now().Truncate(time.Second)
}

func TestFallbackFollowRequiresOptInAndIndependentSampleAndReceiptWindows(t *testing.T) {
	ctx := context.Background()
	s, n, at := newFallbackFollowStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	record := func(sampleSec, receiveSec int) {
		t.Helper()
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(at.Add(time.Duration(sampleSec)*time.Second), "203.0.113.30", false), at.Add(time.Duration(receiveSec)*time.Second)))
	}
	check := func(sec int, want string) {
		t.Helper()
		node, _, err := s.ReconcileNodeAddresses(ctx, n.ID, at.Add(time.Duration(sec)*time.Second))
		must(err)
		if node.PublicAddr != want {
			t.Fatalf("at %d got %s want %s", sec, node.PublicAddr, want)
		}
	}
	record(0, 0)
	record(120, 120)
	check(120, n.PublicAddr)
	yes := true
	must(s.setAddressFollowPolicyAt(ctx, n.ID, true, true, &yes, nil, at.Add(120*time.Second)))
	// Turning on cannot adopt a report already sampled before the policy change.
	record(120, 121)
	check(121, n.PublicAddr)
	record(121, 121)
	record(122, 241)
	check(241, n.PublicAddr)
	record(241, 242)
	check(242, "203.0.113.30")
	obs, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if !obs.DNSPending || obs.Observed.IPv4.Address != "" || obs.Observed.IPv4.ConfirmedAt != 0 || obs.Observed.FallbackFollowIPv4.ConfirmedAt == 0 {
		t.Fatalf("consensus polluted or pending not set: %+v", obs)
	}
	// Simulate reloading the Store and an older panel reading only legacy fields.
	restarted := New(s.db)
	same, err := restarted.NodeAddresses(ctx, n.ID)
	must(err)
	if same.Observed.FallbackFollowIPv4 != obs.Observed.FallbackFollowIPv4 || !same.FallbackAutoIPv4 {
		t.Fatal("follow state did not persist")
	}
	raw, _ := json.Marshal(same.Observed)
	var legacy struct {
		IPv4 AddressFamily `json:"ipv4"`
		IPv6 AddressFamily `json:"ipv6"`
	}
	must(json.Unmarshal(raw, &legacy))
	if legacy.IPv4.ConfirmedAt != 0 || legacy.IPv4.Address != "" {
		t.Fatal("legacy panel could promote fallback")
	}
	// Duplicate reports do not refresh evidence, and stale reconciliation keeps the current entry.
	record(241, 600)
	after, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if after.Observed.FallbackFollowIPv4.LastSuccess != obs.Observed.FallbackFollowIPv4.LastSuccess {
		t.Fatal("duplicate refreshed follow")
	}
	check(1200, "203.0.113.30")
}

func TestFallbackFollowResetsOnBrokenEvidenceAndPolicyBoundaries(t *testing.T) {
	ctx := context.Background()
	s, n, at := newFallbackFollowStore(t)
	yes, no := true, false
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name   string
		change func(*agentproto.PublicAddresses)
	}{
		{"old agent", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.CheckStatus = "" }},
		{"conflict", func(p *agentproto.PublicAddresses) {
			p.FallbackIPv4.CheckStatus = agentproto.PublicAddressFallbackCheckConflict
		}},
		{"missing run", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.RunID = "" }},
		{"bad run", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.RunID = strings.Repeat("z", 32) }},
		{"timeout", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.Address = ""; p.FallbackIPv4.Error = "timeout" }},
		{"missing", func(p *agentproto.PublicAddresses) { p.FallbackIPv4 = nil }},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := at.Add(time.Duration(i) * time.Hour)
			must(s.setAddressFollowPolicyAt(ctx, n.ID, true, true, &no, nil, start))
			must(s.setAddressFollowPolicyAt(ctx, n.ID, true, true, &yes, nil, start))
			send := func(sec int, change func(*agentproto.PublicAddresses)) {
				ts := start.Add(time.Duration(sec) * time.Second)
				p := fallbackFollowSample(ts, "203.0.113.30", false)
				if change != nil {
					change(p)
				}
				must(s.ObserveNodeAddresses(ctx, n.ID, "token", p, ts))
			}
			send(1, nil)
			send(61, tc.change)
			obs, err := s.NodeAddresses(ctx, n.ID)
			must(err)
			if obs.Observed.FallbackFollowIPv4.CandidateSince != 0 {
				t.Fatal("bad evidence kept candidate")
			}
			send(121, nil)
			obs, err = s.NodeAddresses(ctx, n.ID)
			must(err)
			if obs.Observed.FallbackFollowIPv4.ConfirmedAt != 0 {
				t.Fatal("failure did not restart confirmation")
			}
			send(241, nil)
			obs, err = s.NodeAddresses(ctx, n.ID)
			must(err)
			if obs.Observed.FallbackFollowIPv4.ConfirmedAt == 0 {
				t.Fatal("fresh sequence failed")
			}
		})
	}
	// Manual edits disable following and clear the candidate, without changing the optional policy.
	node, err := s.NodeByID(ctx, n.ID)
	must(err)
	node.PublicAddr = "192.0.2.10"
	must(s.UpdateNode(ctx, node))
	obs, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if obs.AutoIPv4 || !obs.FallbackAutoIPv4 || obs.Observed.FallbackFollowIPv4.ConfirmedAt != 0 {
		t.Fatal("manual edit left effective fallback", obs)
	}
	// An old client omitting the fallback flag preserves it, while re-enabling resets evidence.
	must(s.SetAddressPolicy(ctx, n.ID, true, true))
	obs, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if !obs.FallbackAutoIPv4 || obs.Observed.FallbackFollowIPv4.CandidateSince != 0 {
		t.Fatal("legacy update changed policy")
	}
	must(s.ResetPairCode(ctx, n.ID, "again", time.Hour))
	_, err = s.RedeemPairCode(ctx, "again", "new-token", "node", "test", "linux")
	must(err)
	obs, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if obs.Observed.FallbackFollowIPv4.CandidateSince != 0 || obs.Observed.FallbackIPv4 != nil {
		t.Fatal("re-pair kept candidate")
	}
}

func TestFallbackFollowRunAddressOriginAndClockChangesRestartConfirmation(t *testing.T) {
	ctx := context.Background()
	s, n, at := newFallbackFollowStore(t)
	yes, no := true, false
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name   string
		change func(*agentproto.PublicAddresses)
		delay  time.Duration
	}{
		{"run", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.RunID = strings.Repeat("b", 32) }, 0},
		{"origin", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.Origin = "https://other.example.com:2096" }, 0},
		{"address", func(p *agentproto.PublicAddresses) { p.FallbackIPv4.Address = "192.0.2.10" }, 0},
		{"expired", func(p *agentproto.PublicAddresses) {}, 16 * time.Minute},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := at.Add(time.Duration(i) * time.Hour)
			must(s.setAddressFollowPolicyAt(ctx, n.ID, true, false, &no, nil, start))
			must(s.setAddressFollowPolicyAt(ctx, n.ID, true, false, &yes, nil, start))
			first := start.Add(time.Second)
			must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(first, "203.0.113.30", false), first))
			next := start.Add(121*time.Second + tc.delay)
			p := fallbackFollowSample(next, "203.0.113.30", false)
			tc.change(p)
			must(s.ObserveNodeAddresses(ctx, n.ID, "token", p, next))
			obs, err := s.NodeAddresses(ctx, n.ID)
			must(err)
			if obs.Observed.FallbackFollowIPv4.ConfirmedAt != 0 || obs.Observed.FallbackFollowIPv4.CandidateSince != next.Unix() {
				t.Fatal("change retained old confirmation", obs)
			}
		})
	}
	// Backward receipt clock cannot complete or inherit confirmation.
	start := at.Add(8 * time.Hour)
	must(s.setAddressFollowPolicyAt(ctx, n.ID, true, false, &yes, nil, start))
	first := start.Add(time.Second)
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(first, "203.0.113.30", false), first))
	next := start.Add(2 * time.Second)
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(next, "203.0.113.30", false), start))
	obs, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if obs.Observed.FallbackFollowIPv4.ConfirmedAt != 0 {
		t.Fatal("clock rollback confirmed")
	}
}

func TestFallbackFollowIPv6AndPrimaryPriority(t *testing.T) {
	ctx := context.Background()
	s, n, at := newFallbackFollowStore(t)
	yes := true
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.setAddressFollowPolicyAt(ctx, n.ID, true, true, nil, &yes, at))
	for _, sec := range []int{1, 121} {
		ts := at.Add(time.Duration(sec) * time.Second)
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(ts, "2001:db8::30", true), ts))
	}
	node, _, err := s.ReconcileNodeAddresses(ctx, n.ID, at.Add(121*time.Second))
	must(err)
	if node.V6Addr != "2001:db8::30" || node.PublicAddr != n.PublicAddr {
		t.Fatal(node)
	}
	for _, sec := range []int{181, 301} {
		ts := at.Add(time.Duration(sec) * time.Second)
		p := fallbackFollowSample(ts, "2001:db8::30", true)
		p.IPv6 = "2001:db8::10"
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", p, ts))
	}
	node, _, err = s.ReconcileNodeAddresses(ctx, n.ID, at.Add(301*time.Second))
	must(err)
	obs, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if node.V6Addr != "2001:db8::10" || obs.Observed.FallbackFollowIPv6.CandidateSince != 0 {
		t.Fatal("primary not preferred", node, obs)
	}
}

func TestFallbackFollowingRecompilesLinesAndQueuesDNS(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, l := lineFixture(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ib.Protocol = spec.Shadowsocks
	must(s.UpdateInbound(ctx, ib))
	l.Backend, l.Protocol, l.Groups = "realm", "both", l.Groups[:1]
	_, err := s.db.Exec(`UPDATE nodes SET domain='target.example.com' WHERE id=?`, ns[3].ID)
	must(err)
	must(s.SaveLine(ctx, l))
	before, err := s.NodeForwards(ctx, ns[0].ID)
	must(err)
	_, err = s.RedeemPairCode(ctx, "pair-landing", "token", "landing", "test", "linux")
	must(err)
	at := time.Now().Truncate(time.Second)
	yes := true
	must(s.setAddressFollowPolicyAt(ctx, ns[3].ID, true, false, &yes, nil, at))
	for _, sec := range []int{1, 121} {
		ts := at.Add(time.Duration(sec) * time.Second)
		must(s.ObserveNodeAddresses(ctx, ns[3].ID, "token", fallbackFollowSample(ts, "203.0.113.30", false), ts))
	}
	_, pending, err := s.ReconcileNodeAddresses(ctx, ns[3].ID, at.Add(121*time.Second))
	must(err)
	after, err := s.NodeForwards(ctx, ns[0].ID)
	must(err)
	if !pending || len(after) != 1 || after[0].Target != "203.0.113.30:443" || after[0].Tag == before[0].Tag {
		t.Fatal("line or DNS not updated", after, pending)
	}
}

func TestFallbackExpiredOrFutureEvidenceCannotRevive(t *testing.T) {
	ctx := context.Background()
	s, n, at := newFallbackFollowStore(t)
	yes := true
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.setAddressFollowPolicyAt(ctx, n.ID, true, true, &yes, nil, at))
	for _, sec := range []int{1, 121} {
		ts := at.Add(time.Duration(sec) * time.Second)
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(ts, "203.0.113.30", false), ts))
	}
	// Expiry is persisted, so a subsequent clock correction cannot revive this confirmation.
	_, _, err := s.ReconcileNodeAddresses(ctx, n.ID, at.Add(20*time.Minute))
	must(err)
	node, _, err := s.ReconcileNodeAddresses(ctx, n.ID, at.Add(122*time.Second))
	must(err)
	if node.PublicAddr != n.PublicAddr {
		t.Fatal("expired evidence revived", node)
	}
	for _, sec := range []int{1300, 1420} {
		ts := at.Add(time.Duration(sec) * time.Second)
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(ts, "203.0.113.30", false), ts))
	}
	// A new future report must withdraw fallback confirmation without poisoning the dedup cursor.
	future := fallbackFollowSample(at.Add(time.Hour), "203.0.113.30", false)
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", future, at.Add(1421*time.Second)))
	obs, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if obs.Observed.SampledAt != at.Add(1420*time.Second).Unix() || obs.Observed.FallbackFollowIPv4.CandidateSince != 0 {
		t.Fatal("future report kept or poisoned evidence", obs)
	}
}
