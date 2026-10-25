package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type runtimeRig struct {
	t       *testing.T
	ctx     context.Context
	conn    *sql.DB
	store   *store.Store
	state   *AgentState
	service *ServiceStatus
	at      time.Time
	seq     map[int64]uint64
}

func newRuntimeRig(t *testing.T) *runtimeRig {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	state := &AgentState{Store: st}
	return &runtimeRig{t: t, ctx: ctx, conn: conn, store: st, state: state, service: NewServiceStatus(st, state), at: time.Now().UTC().Truncate(time.Second), seq: map[int64]uint64{}}
}
func (r *runtimeRig) node(name, addr string) *domain.Node {
	r.t.Helper()
	n := &domain.Node{Name: name, PublicAddr: addr}
	if err := r.store.CreateNode(r.ctx, n, "pair-"+name, time.Hour); err != nil {
		r.t.Fatal(err)
	}
	n, err := r.store.RedeemPairCode(r.ctx, "pair-"+name, "token-"+name, name, "v0.80.0", "linux/amd64")
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}
func (r *runtimeRig) inbound(n *domain.Node, tag string) *domain.Inbound {
	r.t.Helper()
	ib := &domain.Inbound{NodeID: n.ID, Tag: tag, Protocol: spec.VLESS, Port: 443, Core: "xray", Enabled: true}
	if err := r.store.CreateInbound(r.ctx, ib); err != nil {
		r.t.Fatal(err)
	}
	r.state.Invalidate()
	return ib
}
func (r *runtimeRig) entry(n *domain.Node, ib *domain.Inbound) *domain.Entry {
	r.t.Helper()
	entry := &domain.Entry{Name: ib.Tag, InboundID: ib.ID, DisplayHost: n.PublicAddr, DisplayPort: ib.Port, Enabled: true, Rate: 1}
	if err := r.store.CreateEntry(r.ctx, entry); err != nil {
		r.t.Fatal(err)
	}
	return entry
}
func (r *runtimeRig) report(n *domain.Node, edit func(*agentproto.Report)) agentproto.Report {
	r.t.Helper()
	r.state.Invalidate()
	current, err := r.store.NodeByID(r.ctx, n.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	desired, err := r.state.Build(r.ctx, current, r.at)
	if err != nil {
		r.t.Fatal(err)
	}
	tags := []string{}
	for _, ib := range desired.Node.Inbounds {
		tags = append(tags, ib.Tag)
	}
	caps := spec.CapabilitiesForCore("xray")
	r.seq[n.ID]++
	rep := agentproto.Report{Version: "v0.80.0", Revision: desired.Revision, AppliedRevision: desired.Revision, TrafficEpoch: "runtime-epoch", TrafficSeq: r.seq[n.ID], Cores: map[string]agentproto.CoreStatus{"xray": {Running: true, Capabilities: &caps, Inbounds: tags}}}
	fs, err := r.store.NodeForwards(r.ctx, n.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, f := range fs {
		status := agentproto.ForwardStatus{Tag: f.Tag, Health: "up", ProbeProtocol: "tcp", Up: true}
		if len(f.Targets) > 0 {
			status.Targets = append(status.Targets, agentproto.ForwardTargetStatus{Target: f.Target, Health: "up", ProbeProtocol: "tcp", Up: true})
			for _, target := range f.Targets {
				status.Targets = append(status.Targets, agentproto.ForwardTargetStatus{Target: target.Target, Health: "up", ProbeProtocol: "tcp", Up: true})
			}
		}
		rep.Forwards = append(rep.Forwards, status)
	}
	if edit != nil {
		edit(&rep)
	}
	if _, err := r.store.RecordNodeReport(r.ctx, n.ID, "token-"+n.Name, rep, true, r.at); err != nil {
		r.t.Fatal(err)
	}
	r.state.Invalidate()
	return rep
}
func (r *runtimeRig) evaluate(entries ...*domain.Entry) map[int64]EntryRuntimeStatus {
	r.t.Helper()
	got, err := r.service.Evaluate(r.ctx, entries, r.at)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, v := range got {
		if v.EndToEnd {
			r.t.Fatal("claimed end-to-end success")
		}
	}
	return got
}
func assertRuntime(t *testing.T, got EntryRuntimeStatus, state, reason string) {
	t.Helper()
	if got.State != state || got.Reason != reason {
		t.Fatalf("got %+v, want %s/%s", got, state, reason)
	}
}
func stopCore(rep *agentproto.Report) {
	core := rep.Cores["xray"]
	core.Running = false
	core.Inbounds = nil
	rep.Cores["xray"] = core
}

func TestServiceStatusDirectFreshnessAndConfirmation(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("direct", "192.0.2.10")
	ib := r.inbound(n, "ordinary")
	entry := r.entry(n, ib)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "no_data")
	r.report(n, nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	observed := *r.evaluate(entry)[entry.ID].ObservedAt
	r.at = r.at.Add(time.Second)
	first := r.report(n, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	for i := 0; i < 3; i++ {
		assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	}
	if _, err := r.store.RecordNodeReport(r.ctx, n.ID, "token-direct", first, false, r.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(n, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
	// A process restart does not inherit a cached failure or green result.
	r.service = NewServiceStatus(r.store, r.state)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(n, nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	r.at = r.at.Add(4 * time.Minute)
	if err := r.store.RecordBeat(r.ctx, n.ID, spec.SystemStatus{}, r.at); err != nil {
		t.Fatal(err)
	}
	stale := r.evaluate(entry)[entry.ID]
	assertRuntime(t, stale, "unknown", "stale")
	if stale.ObservedAt == nil || *stale.ObservedAt == r.at.Unix() || *stale.ObservedAt <= observed {
		t.Fatal("resource beat lent its timestamp to runtime")
	}
}

func TestServiceStatusUnsupportedPendingAndExternal(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("direct", "192.0.2.10")
	ib := r.inbound(n, "ordinary")
	entry := r.entry(n, ib)
	r.report(n, func(rep *agentproto.Report) { rep.Cores = nil })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "unsupported")
	r.report(n, func(rep *agentproto.Report) { rep.Revision = "older-applied-revision" })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "config_pending")
	for i := 0; i < 2; i++ {
		r.report(n, func(rep *agentproto.Report) { rep.TrafficSeq = 0; stopCore(rep) })
		assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	}
	r.report(n, nil)
	entry.DisplayHost = "external.example.com"
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "external_path")
	entry.DisplayHost = n.PublicAddr
	entry.DisplayPort = 8443
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "external_path")
}

func TestServiceStatusRequiresIndependentAppliedRevision(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("changed-listener", "192.0.2.10")
	ib := r.inbound(n, "same-tag")
	entry := r.entry(n, ib)
	before := r.report(n, nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	// The next fetched configuration changes a listener without changing its
	// tag. Early apply failure can leave the previous same-tag core running.
	ib.Port = 8443
	entry.DisplayPort = ib.Port
	if err := r.store.UpdateInbound(r.ctx, ib); err != nil {
		t.Fatal(err)
	}
	r.at = r.at.Add(time.Second)
	current := r.report(n, func(rep *agentproto.Report) { rep.AppliedRevision = before.Revision })
	if current.Revision == before.Revision {
		t.Fatal("test failed to change desired revision")
	}
	for range 3 {
		assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "config_pending")
	}
	// An old agent only has the fetched revision. Even a running tag cannot
	// prove it is running the new listener configuration.
	r.report(n, func(rep *agentproto.Report) { rep.AppliedRevision = "" })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "unsupported")
	r.report(n, nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	// Dropping apply confirmation also discards a pending failure sample.
	r.report(n, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	r.report(n, func(rep *agentproto.Report) { stopCore(rep); rep.AppliedRevision = "" })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "unsupported")
	r.report(n, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
}

func TestServiceStatusGroupedLineFollowsCompletePathsAndFixedEntry(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("front", "192.0.2.10")
	other := r.node("other-front", "192.0.2.11")
	b := r.node("branch-one", "198.51.100.20")
	c := r.node("branch-two", "198.51.100.21")
	d := r.node("landing", "203.0.113.30")
	ib := r.inbound(d, "landing")
	line := &store.ForwardLine{Name: "Grouped", Protocol: "tcp", Backend: "relay", TargetInboundID: ib.ID, Groups: []store.LineGroup{{Strategy: "failover", Members: []store.LineMember{{NodeID: a.ID, Port: 18443}, {NodeID: other.ID, Port: 18443}}}, {Strategy: "roundrobin", Members: []store.LineMember{{NodeID: b.ID, Port: 19443, Weight: 1}, {NodeID: c.ID, Port: 19443, Weight: 1}}}}}
	if err := r.store.SaveLine(r.ctx, line); err != nil {
		t.Fatal(err)
	}
	r.state.Invalidate()
	entry := &domain.Entry{Name: "Route", LineID: &line.ID, LineMemberID: line.Groups[0].Members[0].ID, Enabled: true, Rate: 1}
	if err := r.store.ResolveLineEntry(r.ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := r.store.CreateEntry(r.ctx, entry); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*domain.Node{a, other, b, c, d} {
		r.report(n, nil)
	}
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	// A stale alternative must not age the still-usable complete path.
	r.at = r.at.Add(4 * time.Minute)
	for _, n := range []*domain.Node{a, other, c, d} {
		r.report(n, nil)
	}
	partial := r.evaluate(entry)[entry.ID]
	assertRuntime(t, partial, "degraded", "partial_unknown")
	if partial.ObservedAt == nil || *partial.ObservedAt != r.at.Unix() {
		t.Fatalf("stale branch replaced usable path observation: %+v", partial)
	}
	r.report(b, nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	// One reachable intermediate whose own route is broken is not a complete
	// healthy path. The other complete branch keeps the entry degraded.
	fail := func(rep *agentproto.Report) { rep.Forwards[0].Health = "down"; rep.Forwards[0].Up = false }
	r.at = r.at.Add(time.Second)
	r.report(b, fail)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "degraded", "partial_unknown")
	r.at = r.at.Add(time.Second)
	r.report(b, fail)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "degraded", "partial_failure")
	// A's usable edge goes only to broken B; its edge to healthy C has failed.
	// Per-layer "some member is up" must not manufacture an end-to-end route.
	disconnectHealthy := func(rep *agentproto.Report) {
		rep.Forwards[0].Targets[1].Health = "down"
		rep.Forwards[0].Targets[1].Up = false
	}
	r.at = r.at.Add(time.Second)
	r.report(a, disconnectHealthy)
	r.evaluate(entry)
	r.at = r.at.Add(time.Second)
	r.report(a, disconnectHealthy)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "no_route")
	// Fresh confirmed failed edges are enough to break the path even when
	// downstream reports have expired. Preserve the failure observation time.
	r.at = r.at.Add(4 * time.Minute)
	allEdgesDown := func(rep *agentproto.Report) {
		for i := range rep.Forwards[0].Targets {
			rep.Forwards[0].Targets[i].Health = "down"
			rep.Forwards[0].Targets[i].Up = false
		}
	}
	r.report(a, allEdgesDown)
	r.evaluate(entry)
	r.at = r.at.Add(time.Second)
	r.report(a, allEdgesDown)
	broken := r.evaluate(entry)[entry.ID]
	assertRuntime(t, broken, "down", "no_route")
	if broken.ObservedAt == nil || *broken.ObservedAt != r.at.Unix() {
		t.Fatalf("stale downstream replaced confirmed failure observation: %+v", broken)
	}
	// Another healthy first-group member never rescues this fixed entry.
	r.at = r.at.Add(4 * time.Minute)
	for _, n := range []*domain.Node{other, b, c, d} {
		r.report(n, nil)
	}
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "stale")
}

func TestServiceStatusClientChainAndSafeOutput(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("front", "192.0.2.10")
	b := r.node("landing", "203.0.113.30")
	ai := r.inbound(a, "front-listener")
	bi := r.inbound(b, "exit-listener")
	front := r.entry(a, ai)
	exit := r.entry(b, bi)
	exit.ClientDialerEntryID = &front.ID
	if err := r.store.UpdateEntry(r.ctx, exit); err != nil {
		t.Fatal(err)
	}
	r.report(a, nil)
	r.report(b, nil)
	result := r.evaluate(exit)
	if len(result) != 1 {
		t.Fatal("hidden dependency leaked")
	}
	assertRuntime(t, result[exit.ID], "healthy", "healthy")
	if !slices.Contains(result[exit.ID].Evidence, "client_chain") {
		t.Fatal("missing client chain evidence")
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{a.PublicAddr, b.PublicAddr, ai.Tag, bi.Tag, "token-front", "runtime-epoch"} {
		if strings.Contains(string(body), secret) {
			t.Fatal("private dependency escaped runtime projection")
		}
	}
	// Imported/corrupt cycles must be bounded even though normal writes reject them.
	if _, err := r.conn.Exec(`UPDATE entries SET client_dialer_entry_id=? WHERE id=?`, exit.ID, front.ID); err != nil {
		t.Fatal(err)
	}
	assertRuntime(t, r.evaluate(exit)[exit.ID], "unknown", "no_route")
}

func TestServiceStatusNoRequestsOrUserTrafficAreRequired(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("quiet", "192.0.2.10")
	ib := r.inbound(n, "quiet-listener")
	entry := r.entry(n, ib)
	r.report(n, nil)
	got := r.evaluate(entry)[entry.ID]
	assertRuntime(t, got, "healthy", "healthy")
	if got.ObservedAt == nil || *got.ObservedAt != r.at.Unix() {
		t.Fatal(fmt.Sprintf("observation not report time: %+v", got))
	}
}

func TestServiceStatusEmptyAuthenticatedListenersWaitForUsers(t *testing.T) {
	for _, tc := range []struct {
		core     string
		protocol spec.Protocol
	}{{"mita", spec.Mieru}, {"singbox-extended", spec.Mieru}, {"singbox-extended", spec.SSH}} {
		t.Run(tc.core+"/"+string(tc.protocol), func(t *testing.T) {
			r := newRuntimeRig(t)
			n := r.node("empty", "192.0.2.10")
			ib := &domain.Inbound{NodeID: n.ID, Tag: "waiting", Protocol: tc.protocol, Port: 443, Core: tc.core, Enabled: true}
			if err := r.store.CreateInbound(r.ctx, ib); err != nil {
				t.Fatal(err)
			}
			entry := r.entry(n, ib)
			caps := spec.CapabilitiesForCore(tc.core)
			for range 3 {
				r.at = r.at.Add(time.Second)
				r.report(n, func(rep *agentproto.Report) {
					rep.Cores = map[string]agentproto.CoreStatus{tc.core: {Capabilities: &caps}}
				})
				assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "config_pending")
			}
		})
	}
}

func TestServiceStatusWireGuardChecksBothEndpointsAndHandshake(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("wg-front", "192.0.2.10")
	b := r.node("wg-exit", "203.0.113.30")
	base := &agentproto.WireGuardReport{Available: true, Applied: true, ForwardingSupported: true, Forwarding: &agentproto.WireGuardForwardingStatus{State: "synced", CheckedAt: r.at.Unix()}}
	for _, n := range []*domain.Node{a, b} {
		if err := r.store.SetWireGuardReport(r.ctx, n.ID, "token-"+n.Name, base); err != nil {
			t.Fatal(err)
		}
		r.report(n, nil)
	}
	w := store.WireGuardWrite{Link: store.WireGuardLink{ID: "wg-link", TransitID: a.ID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380, AutoForwarding: true}, User: domain.Inbound{NodeID: a.ID, Tag: "wg-user", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true}, Entry: domain.Entry{Name: "WG", DisplayHost: a.PublicAddr, DisplayPort: 18443, Enabled: true}}
	if err := r.store.SaveWireGuardLink(r.ctx, &w); err != nil {
		t.Fatal(err)
	}
	healthy := func(rep *agentproto.Report) {
		copy := *base
		copy.Forwarding = &agentproto.WireGuardForwardingStatus{State: "synced", CheckedAt: r.at.Unix()}
		copy.Links = []agentproto.WireGuardLinkStatus{{Slot: w.Link.Slot, Connected: true, Handshake: r.at.Unix()}}
		rep.WireGuard = &copy
	}
	r.report(a, healthy)
	r.report(b, healthy)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "healthy", "healthy")
	if observed := r.evaluate(&w.Entry)[w.Entry.ID].ObservedAt; observed == nil || *observed != r.at.Unix() {
		t.Fatal("observation does not reflect the live report and firewall checks")
	}
	// Active links recheck forwarding every 15 seconds. A fresh report must
	// not lend its timestamp to an expired firewall check.
	r.at = r.at.Add(time.Second)
	r.report(b, func(rep *agentproto.Report) {
		healthy(rep)
		rep.WireGuard.Forwarding.CheckedAt = r.at.Add(-4 * time.Minute).Unix()
	})
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "unknown", "stale")
	r.report(b, healthy)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "healthy", "healthy")
	// An explicitly disconnected fresh observation can confirm a failure.
	expired := func(rep *agentproto.Report) {
		healthy(rep)
		rep.WireGuard.Links[0].Handshake = r.at.Add(-4 * time.Minute).Unix()
		rep.WireGuard.Links[0].Connected = false
	}
	r.at = r.at.Add(time.Second)
	r.report(a, expired)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "unknown", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(a, expired)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "down", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(a, healthy)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "healthy", "healthy")
	firewall := func(rep *agentproto.Report) {
		healthy(rep)
		rep.WireGuard.Forwarding = &agentproto.WireGuardForwardingStatus{State: "error", Reason: "forwarding_check_failed", CheckedAt: r.at.Unix()}
	}
	r.at = r.at.Add(time.Second)
	r.report(b, firewall)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "unknown", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(b, firewall)
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "down", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(b, func(rep *agentproto.Report) { healthy(rep); rep.Revision = "previous-policy" })
	assertRuntime(t, r.evaluate(&w.Entry)[w.Entry.ID], "unknown", "config_pending")
}

func TestServiceStatusReverseSamplingUnknownAndExitFailure(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("reverse-front", "192.0.2.10")
	b := r.node("reverse-exit", "203.0.113.30")
	r.report(a, nil)
	r.report(b, nil)
	writes := []store.ReverseWrite{{Link: store.ReverseLink{ID: "reverse-link", TransitID: a.ID, ExitID: b.ID}, User: domain.Inbound{NodeID: a.ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 443, Core: "xray", Enabled: true, Settings: spec.Inbound{Reverse: &spec.ReverseInbound{ID: "reverse-link"}}}, Receiver: domain.Inbound{NodeID: a.ID, Tag: "reverse-control", Protocol: spec.VLESS, Port: 8443, Core: "xray", Enabled: true, Settings: spec.Inbound{Reverse: &spec.ReverseInbound{ID: "reverse-link", Receiver: true, UUID: "example-identity"}, TLS: &spec.TLS{Mode: spec.TLSReality, ServerName: "example.com", Reality: &spec.Reality{PublicKey: "example-public-key"}}}}, Entry: domain.Entry{Name: "Reverse", DisplayHost: a.PublicAddr, DisplayPort: 443, Enabled: true}}}
	if err := r.store.SaveReverseLinks(r.ctx, b.ID, map[string]int64{}, writes); err != nil {
		t.Fatal(err)
	}
	healthy := func(rep *agentproto.Report) {
		core := rep.Cores["xray"]
		connected := true
		core.Reverse = map[string]*bool{"reverse-link": &connected}
		rep.Cores["xray"] = core
	}
	r.report(a, healthy)
	r.report(b, func(rep *agentproto.Report) {
		core := rep.Cores["xray"]
		core.Reverse = map[string]*bool{"reverse-link": nil}
		rep.Cores["xray"] = core
	})
	entry := &writes[0].Entry
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	r.at = r.at.Add(time.Second)
	r.report(a, func(rep *agentproto.Report) {
		core := rep.Cores["xray"]
		core.Reverse = map[string]*bool{"reverse-link": nil}
		rep.Cores["xray"] = core
	})
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "no_data")
	r.at = r.at.Add(time.Second)
	r.report(a, healthy)
	r.report(b, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	r.at = r.at.Add(time.Second)
	r.report(b, stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
}

func TestServiceStatusFailureConfirmationAllowsSkippedReportsAndResetsUnknown(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("polling", "192.0.2.10")
	ib := r.inbound(n, "polling-inbound")
	entry := r.entry(n, ib)
	send := func(edit func(*agentproto.Report)) { r.at = r.at.Add(time.Second); r.report(n, edit) }
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	// The browser missed an intermediate full report; a second distinct fresh
	// negative observation still confirms failure without requiring clock lockstep.
	send(stopCore)
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
	send(nil)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	send(func(rep *agentproto.Report) { rep.Cores = nil })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "unsupported")
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
	send(func(rep *agentproto.Report) { stopCore(rep); rep.Revision = "older-revision" })
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "config_pending")
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	send(stopCore)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
	nextEpoch := func(rep *agentproto.Report) { stopCore(rep); rep.TrafficEpoch = "replacement-journal" }
	send(nextEpoch)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	for range 3 {
		assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
	}
	send(nextEpoch)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "down", "component_failure")
	r.at = r.at.Add(4 * time.Minute)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "stale")
	send(nextEpoch)
	assertRuntime(t, r.evaluate(entry)[entry.ID], "unknown", "component_failure")
}
