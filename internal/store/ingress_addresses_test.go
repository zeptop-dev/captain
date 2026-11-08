package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func ingressFollowFixture(t *testing.T) (*Store, *domain.Node, *Ingress, *domain.Inbound, *domain.Entry, *domain.Entry, *ForwardLine) {
	t.Helper()
	ctx := context.Background()
	s, ns, ib, line := lineFixture(t)
	n := ns[3]
	n.V6Addr = "2001:db8::10"
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	g := &Ingress{NodeID: n.ID, Name: "NAT", Kind: "nat", EntryAddressSource: IngressAddressIPv4, EntryHost: "203.0.113.99", PortMappings: []spec.PortMapping{{LocalFrom: 443, LocalTo: 444, PublicFrom: 30443}}}
	if err := s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	ib.IngressID = &g.ID
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	follow := &domain.Entry{Name: "Following", InboundID: ib.ID, FollowIngress: true, Enabled: true}
	fixed := &domain.Entry{Name: "Manual", InboundID: ib.ID, DisplayHost: "fixed.example.com", DisplayPort: 8443, Enabled: true}
	for _, e := range []*domain.Entry{follow, fixed} {
		if err := s.CreateEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	return s, n, g, ib, follow, fixed, line
}

func TestIngressAddressInheritanceUpdatesSubscribersAndLinesAtomically(t *testing.T) {
	ctx := context.Background()
	s, n, g, ib, follow, fixed, line := ingressFollowFixture(t)
	if g.EntryHost != n.PublicAddr || follow.DisplayHost != n.PublicAddr || follow.DisplayPort != 30443 {
		t.Fatal("source did not resolve", g, follow)
	}
	n.PublicAddr = "198.51.100.20"
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, _ := s.IngressByID(ctx, g.ID)
	e, _ := s.EntryByID(ctx, follow.ID)
	manual, _ := s.EntryByID(ctx, fixed.ID)
	if got.EntryHost != n.PublicAddr || e.DisplayHost != n.PublicAddr || e.DisplayPort != 30443 || manual.DisplayHost != fixed.DisplayHost || manual.DisplayPort != fixed.DisplayPort {
		t.Fatal(got, e, manual)
	}
	fs, err := s.NodeForwards(ctx, line.Groups[1].Members[0].NodeID)
	if err != nil || len(fs) != 1 || fs[0].Target != "198.51.100.20:30443" {
		t.Fatal("line target stale", fs, err)
	}
	g.EntryDomain = "nat.example.com"
	if err = s.UpdateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	e, _ = s.EntryByID(ctx, follow.ID)
	if e.DisplayHost != "nat.example.com" {
		t.Fatal(e)
	}
	g.EntryAddressSource = IngressAddressIPv6
	if err = s.UpdateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	got, _ = s.IngressByID(ctx, g.ID)
	if got.EntryHost != n.V6Addr {
		t.Fatal(got)
	}
	ib.Port = 444
	if err = s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	e, _ = s.EntryByID(ctx, follow.ID)
	if e.DisplayPort != 30444 {
		t.Fatal(e)
	}
	// Losing the mapped inbound cannot leave an apparently-following stale entry.
	ib.IngressID = nil
	if err = s.UpdateInbound(ctx, ib); !errors.Is(err, ErrEntryIngress) {
		t.Fatal(err)
	}
	saved, _ := s.InboundByID(ctx, ib.ID)
	if saved.IngressID == nil {
		t.Fatal("partial inbound edit")
	}
}

func TestIngressAddressMissingFamilyAndWriteFailureRollbackEverything(t *testing.T) {
	ctx := context.Background()
	s, n, g, _, follow, _, _ := ingressFollowFixture(t)
	original := n.PublicAddr
	for _, bad := range []string{"", "2001:db8::20", "0.0.0.0", "invalid"} {
		n.PublicAddr = bad
		if err := s.UpdateNode(ctx, n); !errors.Is(err, ErrIngressAddress) {
			t.Fatalf("%q: %v", bad, err)
		}
		saved, _ := s.NodeByID(ctx, n.ID)
		got, _ := s.IngressByID(ctx, g.ID)
		e, _ := s.EntryByID(ctx, follow.ID)
		if saved.PublicAddr != original || got.EntryHost != original || e.DisplayHost != original {
			t.Fatal("partial address edit", saved, got, e)
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_follow BEFORE UPDATE OF display_host ON entries WHEN NEW.follow_ingress=1 BEGIN SELECT RAISE(ABORT,'test subscriber failure'); END`); err != nil {
		t.Fatal(err)
	}
	n.PublicAddr = "192.0.2.10"
	if err := s.UpdateNode(ctx, n); err == nil {
		t.Fatal("injected failure ignored")
	}
	saved, _ := s.NodeByID(ctx, n.ID)
	got, _ := s.IngressByID(ctx, g.ID)
	if saved.PublicAddr != original || got.EntryHost != original {
		t.Fatal("partial transaction", saved, got)
	}
}

func TestIngressAddressConfirmedNormalAndFallbackFollow(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "fallback"}[fallback], func(t *testing.T) {
			ctx := context.Background()
			s, n, at := newFallbackFollowStore(t)
			inherited := &Ingress{NodeID: n.ID, Name: "follow", Kind: "nat", EntryAddressSource: IngressAddressIPv4}
			fixed := &Ingress{NodeID: n.ID, Name: "fixed", Kind: "nat", EntryHost: "192.0.2.10"}
			for _, g := range []*Ingress{inherited, fixed} {
				if err := s.CreateIngress(ctx, g); err != nil {
					t.Fatal(err)
				}
			}
			yes := true
			if fallback {
				if err := s.setAddressFollowPolicyAt(ctx, n.ID, true, true, &yes, nil, at.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			for _, sec := range []int{0, 60, 120, 300} {
				ts := at.Add(time.Duration(sec) * time.Second)
				sample := &agentproto.PublicAddresses{SampledAt: ts.Unix(), IPv4: "203.0.113.30"}
				if fallback {
					sample = fallbackFollowSample(ts, "203.0.113.30", false)
				}
				if err := s.ObserveNodeAddresses(ctx, n.ID, "token", sample, ts); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.ReconcileNodeAddresses(ctx, n.ID, ts); err != nil {
					t.Fatal(err)
				}
				got, _ := s.IngressByID(ctx, inherited.ID)
				if sec == 0 && got.EntryHost != n.PublicAddr {
					t.Fatal("unconfirmed sample published", got)
				}
			}
			got, _ := s.IngressByID(ctx, inherited.ID)
			custom, _ := s.IngressByID(ctx, fixed.ID)
			if got.EntryHost != "203.0.113.30" || custom.EntryHost != fixed.EntryHost {
				t.Fatal(got, custom)
			}
		})
	}
}

func TestIngressAddressActiveLandingRolloutBlocksNodeChange(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	n := r.nodes[3]
	g := &Ingress{NodeID: n.ID, Name: "NAT landing", Kind: "nat", EntryAddressSource: IngressAddressIPv4}
	if err := r.s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	r.ib.IngressID = &g.ID
	if err := r.s.UpdateInbound(ctx, r.ib); err != nil {
		t.Fatal(err)
	}
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "running" {
		t.Fatal(p)
	}
	old := n.PublicAddr
	n.PublicAddr = "198.51.100.20"
	if err = r.s.UpdateNode(ctx, n); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("changed reserved ingress", err)
	}
	got, _ := r.s.NodeByID(ctx, n.ID)
	if got.PublicAddr != old {
		t.Fatal("partial server change")
	}
}

func TestIngressAddressTopologyUpdatesInheritedSubscribers(t *testing.T) {
	ctx := context.Background()
	s, _, _, ib, follow, _, _ := ingressFollowFixture(t)
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, v := range catalog.Inbounds {
		if v.ID == ib.ID {
			revision = v.Revision
		}
	}
	draft := &TopologyChangeset{Name: "port", Operations: []TopologyOperation{{Key: "port", Kind: "inbound", ID: ib.ID, Revision: revision, Inbound: &TopologyInboundPatch{Port: topologyPtr(444)}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, draft.ID, draft.Version, preview.Revision); err != nil {
		t.Fatal(err)
	}
	e, _ := s.EntryByID(ctx, follow.ID)
	if !e.FollowIngress || e.DisplayPort != 30444 {
		t.Fatal(e)
	}
}

func TestIngressAddressTopologyMetadataPreservesBinding(t *testing.T) {
	ctx := context.Background()
	s, _, _, ib, e, _, _ := ingressFollowFixture(t)
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, v := range catalog.Entries {
		if v.ID == e.ID {
			revision = v.Revision
		}
	}
	// Existing UI always serializes line_id:0, inbound_id and the visible endpoint,
	// even for a name-only edit, and does not know about follow_ingress.
	draft := &TopologyChangeset{Name: "rename", Operations: []TopologyOperation{{Key: "entry", Kind: "entry", ID: e.ID, Revision: revision, Entry: &TopologyEntryPatch{Name: topologyPtr("Renamed"), InboundID: &ib.ID, LineID: topologyPtr(int64(0)), DisplayHost: &e.DisplayHost, DisplayPort: &e.DisplayPort}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, draft.ID, draft.Version, preview.Revision); err != nil {
		t.Fatal(err)
	}
	got, _ := s.EntryByID(ctx, e.ID)
	if !got.FollowIngress {
		t.Fatal("metadata-only legacy topology edit detached ingress following")
	}
}

func TestIngressAddressDNSBookkeepingDoesNotInvalidateTopologyPreview(t *testing.T) {
	ctx := context.Background()
	s, n, g, _, e, _, _ := ingressFollowFixture(t)
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, v := range catalog.Entries {
		if v.ID == e.ID {
			revision = v.Revision
		}
	}
	draft := &TopologyChangeset{Name: "rename", Operations: []TopologyOperation{{Key: "entry", Kind: "entry", ID: e.ID, Revision: revision, Entry: &TopologyEntryPatch{Name: topologyPtr("Renamed")}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BeginIngressDNS(ctx, g.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishIngressDNS(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, draft.ID, draft.Version, preview.Revision); err != nil {
		t.Fatal("DNS bookkeeping invalidated unchanged topology", err)
	}
	// The source address remains configuration and must invalidate a new preview.
	catalog, _ = s.TopologyChangesetCatalog(ctx)
	for _, v := range catalog.Entries {
		if v.ID == e.ID {
			revision = v.Revision
		}
	}
	next := &TopologyChangeset{Name: "rename again", Operations: []TopologyOperation{{Key: "entry", Kind: "entry", ID: e.ID, Revision: revision, Entry: &TopologyEntryPatch{Name: topologyPtr("Again")}}}}
	if err = s.SaveTopologyChangeset(ctx, next); err != nil {
		t.Fatal(err)
	}
	preview, err = s.PreviewTopologyChangeset(ctx, next.ID, next.Version)
	if err != nil {
		t.Fatal(err)
	}
	n.PublicAddr = "203.0.113.30"
	if err = s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, next.ID, next.Version, preview.Revision); !errors.Is(err, ErrTopologyConflict) {
		t.Fatal("address change accepted stale preview", err)
	}
}
