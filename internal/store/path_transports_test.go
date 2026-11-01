package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func pathTransportFixture(t *testing.T, protocol, entry string, backends []string) (*Store, []*domain.Node, *domain.Inbound, *PathRoute) {
	t.Helper()
	ctx := context.Background()
	s, ns, ib, route := pathFixture(t)
	// The final proxy landing is not a new forwarding server and may be older.
	for _, n := range ns[:3] {
		if _, err := s.db.Exec(`UPDATE nodes SET version='v0.88.0' WHERE id=?`, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	ib.Protocol = map[string]spec.Protocol{"tcp": spec.VLESS, "udp": spec.Hysteria2, "both": spec.Shadowsocks}[protocol]
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	route.EntryBackend = entry
	for i := range route.Branches {
		branch := &route.Branches[i]
		p, err := s.ForwardPath(ctx, branch.PathID, branch.PathRevision)
		if err != nil {
			t.Fatal(err)
		}
		for j := range p.Steps {
			p.Steps[j].Backend = backends[j%len(backends)]
		}
		if err = s.SaveForwardPath(ctx, p); err != nil {
			t.Fatal(err)
		}
		branch.PathRevision = p.Version
	}
	return s, ns, ib, route
}

func TestPathTransportMixedProtocolsAndBackends(t *testing.T) {
	for _, protocol := range []string{"tcp", "udp", "both"} {
		for _, entry := range []string{"relay", "realm", "nft"} {
			t.Run(protocol+"/"+entry, func(t *testing.T) {
				ctx := context.Background()
				s, ns, _, r := pathTransportFixture(t, protocol, entry, []string{"realm", "nft"})
				if entry != "relay" {
					r.Branches = r.Branches[:1]
				}
				r.Protocol = "client-cannot-set-this"
				preview, err := s.PreviewPathRoute(ctx, r)
				if err != nil {
					t.Fatal(err)
				}
				if preview.Draft.Protocol != protocol {
					t.Fatalf("read-only protocol = %q", preview.Draft.Protocol)
				}
				*r = preview.Draft
				r.PreviewRevision = preview.Revision
				if err = s.SavePathRoute(ctx, r); err != nil {
					t.Fatal(err)
				}
				got, err := s.PathRoute(ctx, r.ID)
				if err != nil || got.Protocol != protocol {
					t.Fatalf("stored projection = %+v, %v", got, err)
				}
				line, err := s.LineByID(ctx, r.ID)
				if err != nil || line.Protocol != protocol || line.Backend != entry {
					t.Fatalf("backing line = %+v, %v", line, err)
				}
				root := pathOwned(t, s, ns[0].ID, r.ID)[0]
				if root.Protocol != protocol || root.Backend != pathWireBackend(entry) || len(root.Hops()) != len(r.Branches) {
					t.Fatalf("wrong entry %+v", root)
				}
				for i, b := range r.Branches {
					if root.Hops()[i].Target != net.JoinHostPort(ns[i+1].PublicAddr, fmt.Sprint(b.Ports[0])) {
						t.Fatalf("branch %d selected wrong first hop: %+v", i, root.Hops())
					}
					p, err := s.ForwardPath(ctx, b.PathID, b.PathRevision)
					if err != nil {
						t.Fatal(err)
					}
					for j, hop := range p.Hops {
						found := false
						for _, f := range pathOwned(t, s, hop.NodeID, r.ID) {
							if f.LineMemberID != pathKey(b.ID, hop.Key) {
								continue
							}
							found = true
							if f.Protocol != protocol || f.Backend != pathWireBackend(hop.Backend) || len(f.Targets) != 0 || f.PathHealth != nil {
								t.Fatalf("wrong hop %+v", f)
							}
							want := net.JoinHostPort(ns[3].PublicAddr, "443")
							if j+1 < len(p.Hops) {
								want = net.JoinHostPort(ns[2].PublicAddr, fmt.Sprint(b.Ports[j+1]))
							}
							if f.Target != want {
								t.Fatalf("crossed complete path: got %s want %s", f.Target, want)
							}
						}
						if !found {
							t.Fatal("missing dedicated hop")
						}
					}
				}
			})
		}
	}
}

func TestPathTransportPreservesBackendIntentAndPinnedReferences(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathTransportFixture(t, "tcp", "realm", []string{"nft", "relay"})
	r.Branches = r.Branches[:1]
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.EntryBackend = ""
	r.Name = "Old editor rename"
	if err := s.SavePathRoute(ctx, r); err != nil || r.EntryBackend != "realm" {
		t.Fatalf("omitted entry backend lost: %+v %v", r, err)
	}
	raw, _ := json.Marshal(r)
	var old map[string]any
	_ = json.Unmarshal(raw, &old)
	old["entry_backend"] = nil
	raw, _ = json.Marshal(old)
	var decoded PathRoute
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	*r = decoded
	if err := s.SavePathRoute(ctx, r); err != nil || r.EntryBackend != "realm" {
		t.Fatalf("null entry backend lost: %v", err)
	}
	r.EntryBackend = "relay"
	if err := s.SavePathRoute(ctx, r); err != nil || pathOwned(t, s, ns[0].ID, r.ID)[0].Backend != "" {
		t.Fatalf("explicit relay reset: %v", err)
	}
	p, err := s.ForwardPath(ctx, r.Branches[0].PathID, r.Branches[0].PathRevision)
	if err != nil {
		t.Fatal(err)
	}
	pinned := p.Version
	p.Steps[0].Backend = ""
	p.Name = "Old editor"
	if err = s.SaveForwardPath(ctx, p); err != nil || p.Steps[0].Backend != "nft" || p.Hops[0].Backend != "nft" {
		t.Fatalf("stable step lost backend: %+v %v", p, err)
	}
	p.Steps[0].Backend = "relay"
	if err = s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	reference := &ForwardPath{Name: "Fixed older path", Steps: []PathStep{{ID: "reference", PathID: p.ID, PathRevision: pinned}}}
	if err = s.SaveForwardPath(ctx, reference); err != nil || reference.Hops[0].Backend != "nft" {
		t.Fatalf("pinned backend followed live template: %+v %v", reference, err)
	}
	oldPath, err := s.ForwardPath(ctx, p.ID, pinned)
	if err != nil || oldPath.Hops[0].Backend != "nft" {
		t.Fatal("old revision rewritten", err)
	}
	// The same step ID on another node is a new source, not inherited policy.
	p.Steps[0].Backend = "realm"
	if err = s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.Steps[0].NodeID = ns[0].ID
	p.Steps[0].Backend = ""
	if err = s.SaveForwardPath(ctx, p); err != nil || p.Steps[0].Backend != "" || p.Hops[0].Backend != "" {
		t.Fatalf("backend crossed source identity: %+v %v", p, err)
	}
	for _, step := range []PathStep{{PathID: p.ID, PathRevision: p.Version, Backend: "nft"}, {LineID: r.ID, LineVersion: r.Version, Backend: "relay"}, {NodeID: ns[1].ID, Backend: "bad"}} {
		if err = s.SaveForwardPath(ctx, &ForwardPath{Name: "Invalid override", Steps: []PathStep{step}}); err == nil {
			t.Fatal("invalid source/backend accepted")
		}
	}
}

func TestPathTransportSourceLinePinsBackendAndDefaultLanding(t *testing.T) {
	ctx := context.Background()
	s, ns, _, line := lineFixture(t)
	line.Backend = "nft"
	line.Groups = line.Groups[:1]
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	step := PathStep{ID: "line", LineID: line.ID, LineVersion: line.Version, IncludeLanding: true}
	p := &ForwardPath{Name: "Pinned nft source", Steps: []PathStep{step}}
	if err := s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	if len(p.Hops) != 2 || p.Hops[0].Backend != "nft" || p.Hops[1].Backend != "" || p.Hops[1].NodeID != ns[3].ID {
		t.Fatalf("source backend/landing policy: %+v", p.Hops)
	}
	line.Backend = "realm"
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	p2 := &ForwardPath{Name: "Same immutable source", Steps: []PathStep{step}}
	if err := s.SaveForwardPath(ctx, p2); err != nil || p2.Hops[0].Backend != "nft" {
		t.Fatalf("source backend pin changed: %+v %v", p2, err)
	}
}

func TestPathTransportRejectsUnsupportedSelectionAndVersions(t *testing.T) {
	for _, tc := range []struct{ name, protocol, entry, strategy string }{
		{"realm candidates", "tcp", "realm", "roundrobin"}, {"nft candidates", "tcp", "nft", "roundrobin"},
		{"udp failover", "udp", "relay", "failover"}, {"both failover", "both", "relay", "failover"},
		{"invalid backend", "tcp", "invalid", "roundrobin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, r := pathTransportFixture(t, tc.protocol, tc.entry, []string{"relay"})
			r.Strategy = tc.strategy
			if err := s.SavePathRoute(context.Background(), r); err == nil {
				t.Fatal("unsupported choice accepted")
			}
			if r.ID != 0 {
				t.Fatal("failed save published identity")
			}
		})
	}
	for _, tc := range []struct{ protocol, hop string }{{"udp", "relay"}, {"both", "relay"}, {"tcp", "nft"}, {"tcp", "realm"}} {
		for node := 0; node < 3; node++ {
			t.Run(fmt.Sprintf("%s/%s/node%d", tc.protocol, tc.hop, node), func(t *testing.T) {
				s, ns, _, r := pathTransportFixture(t, tc.protocol, "relay", []string{tc.hop})
				if _, err := s.db.Exec(`UPDATE nodes SET version='v0.87.0' WHERE id=?`, ns[node].ID); err != nil {
					t.Fatal(err)
				}
				if err := s.SavePathRoute(context.Background(), r); !errors.Is(err, ErrPathTransportVersion) {
					t.Fatalf("old forwarding node accepted: %v", err)
				}
			})
		}
	}
}

func TestPathTransportUsesActualAdjacentAddressFamilies(t *testing.T) {
	t.Run("nft entry checks first hop not landing", func(t *testing.T) {
		s, ns, ib, r := pathTransportFixture(t, "tcp", "nft", []string{"relay"})
		r.Branches = r.Branches[:1]
		if _, err := s.db.Exec(`UPDATE nodes SET public_addr='',v6_addr='2001:db8::30' WHERE id=?`, ib.NodeID); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePathRoute(context.Background(), r); err != nil {
			t.Fatal("entry incorrectly validated against remote IPv6 landing", err)
		}
		entry := pathOwned(t, s, ns[0].ID, r.ID)[0]
		if entry.Listen != "0.0.0.0" || entry.Target != "198.51.100.20:22001" {
			t.Fatalf("wrong nft adjacent edge: %+v", entry)
		}
	})
	t.Run("realm UDP chooses first hop family", func(t *testing.T) {
		s, ns, _, r := pathTransportFixture(t, "udp", "realm", []string{"relay"})
		r.Branches = r.Branches[:1]
		if _, err := s.db.Exec(`UPDATE nodes SET public_addr='',v6_addr='2001:db8::10' WHERE id=?`, ns[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE nodes SET v6_addr='2001:db8::20',domain='example.com' WHERE id=?`, ns[1].ID); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePathRoute(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		entry := pathOwned(t, s, ns[0].ID, r.ID)[0]
		if entry.Listen != "::" || entry.Target != "[2001:db8::20]:22001" {
			t.Fatalf("realm used final landing family: %+v", entry)
		}
	})
	t.Run("mixed nft cross-family rejected atomically", func(t *testing.T) {
		s, ns, _, r := pathTransportFixture(t, "both", "relay", []string{"nft", "relay"})
		if _, err := s.db.Exec(`UPDATE nodes SET public_addr='',v6_addr='2001:db8::30' WHERE id=?`, ns[2].ID); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePathRoute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "same address family") {
			t.Fatalf("cross-family nft accepted: %v", err)
		}
		for _, n := range ns {
			fs, err := s.NodeForwards(context.Background(), n.ID)
			if err != nil || len(fs) != 0 {
				t.Fatal("failed compile leaked rules", err)
			}
		}
	})
}

func TestPathTransportHealthRequiresEveryHopNativeTCP(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, r := pathTransportFixture(t, "tcp", "relay", []string{"relay"})
	pathHealthCapabilityFixture(t, s, ns[0].ID, time.Now().UTC())
	if _, err := s.db.Exec(`UPDATE nodes SET version='v0.88.0' WHERE id=?`, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	r.Health = &PathRouteHealth{Enabled: true, ServerName: "example.com"}
	ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard}
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	before := pathOwned(t, s, ns[0].ID, r.ID)
	p, err := s.ForwardPath(ctx, r.Branches[0].PathID, r.Branches[0].PathRevision)
	if err != nil {
		t.Fatal(err)
	}
	p.Steps[1].Backend = "realm"
	if err = s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	r.Branches[0].PathRevision = p.Version
	r.Health = nil
	if err = s.SavePathRoute(ctx, r); err == nil || !strings.Contains(err.Error(), "entirely native") {
		t.Fatalf("non-native deep hop kept health: %v", err)
	}
	stored, err := s.PathRoute(ctx, r.ID)
	if err != nil || stored.Health == nil || !stored.Health.Enabled || !reflect.DeepEqual(before, pathOwned(t, s, ns[0].ID, r.ID)) {
		t.Fatal("failed edit cleared health or changed rules", err)
	}
	ib.Protocol = spec.Hysteria2
	if err = s.UpdateInbound(ctx, ib); err == nil {
		t.Fatal("UDP recompile silently kept/removed old TCP health")
	}
}

func TestPathTransportRejectsSpecialLandingsAndSelfHops(t *testing.T) {
	for _, kind := range []string{"mieru-both", "proxy", "wireguard", "reverse", "landing-as-hop"} {
		t.Run(kind, func(t *testing.T) {
			s, ns, ib, r := pathTransportFixture(t, "tcp", "nft", []string{"relay"})
			r.Branches = r.Branches[:1]
			switch kind {
			case "mieru-both":
				ib.Protocol = spec.Mieru
				ib.Settings.MieruTransport = "BOTH"
			case "proxy":
				ib.Settings.AcceptProxyProtocol = true
			case "wireguard":
				ib.Settings.WireGuardSlot = 1
			case "reverse":
				ib.Settings.Reverse = &spec.ReverseInbound{Receiver: true}
			case "landing-as-hop":
				p, err := s.ForwardPath(context.Background(), r.Branches[0].PathID, r.Branches[0].PathRevision)
				if err != nil {
					t.Fatal(err)
				}
				p.Steps = p.Steps[:1]
				p.Steps[0].NodeID = ns[3].ID
				if err = s.SaveForwardPath(context.Background(), p); err != nil {
					t.Fatal(err)
				}
				r.Branches[0].PathRevision = p.Version
				r.Branches[0].Ports = r.Branches[0].Ports[:1]
			}
			// Store the controlled legacy/special shape directly; this assertion
			// targets the path compiler independently of inbound editor validation.
			raw, _ := json.Marshal(ib.Settings)
			if _, err := s.db.Exec(`UPDATE inbounds SET protocol=?,settings_json=? WHERE id=?`, ib.Protocol, string(raw), ib.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.SavePathRoute(context.Background(), r); err == nil {
				t.Fatal("unsupported special landing accepted")
			}
		})
	}
}

func TestPathTransportLandingRecompileAndDisjointListeners(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, r := pathTransportFixture(t, "tcp", "relay", []string{"relay"})
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	old := pathOwned(t, s, ns[0].ID, r.ID)[0]
	ib.Protocol = spec.Hysteria2
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	got, err := s.PathRoute(ctx, r.ID)
	if err != nil || got.Protocol != "udp" || got.EntryBackend != "relay" {
		t.Fatalf("derived protocol did not follow landing: %+v %v", got, err)
	}
	current := pathOwned(t, s, ns[0].ID, r.ID)[0]
	if current.Protocol != "udp" || current.Tag == old.Tag {
		t.Fatal("runtime identity did not track actual protocol")
	}
	// A separate TCP listener may use the same numeric port. A both listener may not.
	tcp := &domain.Inbound{NodeID: ns[3].ID, Tag: "separate-tcp", Protocol: spec.VLESS, Port: 4443, Enabled: true}
	if err = s.CreateInbound(ctx, tcp); err != nil {
		t.Fatal(err)
	}
	other := &ForwardLine{Name: "Disjoint TCP", Backend: "relay", Protocol: "tcp", TargetInboundID: tcp.ID, Groups: []LineGroup{{Members: []LineMember{{NodeID: ns[0].ID, Port: r.Entry.Port}}}}}
	if err = s.SaveLine(ctx, other); err != nil {
		t.Fatal("disjoint transport collision", err)
	}
	ib.Protocol = spec.Shadowsocks
	if err = s.UpdateInbound(ctx, ib); err == nil {
		t.Fatal("both transport stole TCP listener")
	}
	stored, _ := s.InboundByID(ctx, ib.ID)
	if stored.Protocol != spec.Hysteria2 || !reflect.DeepEqual(current, pathOwned(t, s, ns[0].ID, r.ID)[0]) {
		t.Fatal("failed protocol recompile did not roll back")
	}
}

func TestPathTransportTopologyDraftCannotBypassPathLandingOwnership(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, r := pathTransportFixture(t, "tcp", "relay", []string{"relay"})
	pathHealthCapabilityFixture(t, s, ns[0].ID, time.Now().UTC())
	r.Health = &PathRouteHealth{Enabled: true, ServerName: "example.com"}
	ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard}
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	before := pathOwned(t, s, ns[0].ID, r.ID)
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var entry TopologyInboundCatalog
	for _, v := range catalog.Inbounds {
		if v.ID == ib.ID {
			entry = v
		}
	}
	if entry.Editable {
		t.Fatal("path landing became a topology edit target")
	}
	draft := &TopologyChangeset{Name: "Cannot change path landing", Operations: []TopologyOperation{{Key: "landing", Kind: "inbound", ID: ib.ID, Revision: entry.Revision, Inbound: &TopologyInboundPatch{Port: topologyPtr(9443), Protocol: spec.Hysteria2}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); !errors.Is(err, ErrTopologyUnsupported) {
		t.Fatalf("existing inbound protocol edit unexpectedly accepted: %v", err)
	}
	draft.Operations[0].Inbound.Protocol = ""
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version); err == nil {
		t.Fatal("preview bypassed path ownership")
	}
	if _, err = s.CommitTopologyChangeset(ctx, draft.ID, draft.Version, "untrusted-preview"); err == nil {
		t.Fatal("commit bypassed path ownership")
	}
	stored, _ := s.InboundByID(ctx, ib.ID)
	route, _ := s.PathRoute(ctx, r.ID)
	if stored.Port != ib.Port || stored.Protocol != ib.Protocol || route.Health == nil || !route.Health.Enabled || !reflect.DeepEqual(before, pathOwned(t, s, ns[0].ID, r.ID)) {
		t.Fatal("rejected topology mutation changed path")
	}
}

func TestPathTransportLoadNodeReservesActualFutureProtocols(t *testing.T) {
	ctx := context.Background()
	rig, draft := pathRolloutRig(t)
	plan, err := rig.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Model the persisted future rules of a new transport plan. The plan machinery
	// is tested separately; this regression guards the compiler's reservation load.
	for _, protocol := range []string{"udp", "both"} {
		rows, err := rig.s.db.Query(`SELECT node_id,after_json FROM line_rollout_nodes WHERE plan_id=?`, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		updates := map[int64]string{}
		for rows.Next() {
			var node int64
			var raw string
			if err = rows.Scan(&node, &raw); err != nil {
				t.Fatal(err)
			}
			var fs []LineChangeForward
			if err = json.Unmarshal([]byte(raw), &fs); err != nil {
				t.Fatal(err)
			}
			for i := range fs {
				fs[i].Protocol = protocol
			}
			encoded, _ := json.Marshal(fs)
			updates[node] = string(encoded)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		for node, raw := range updates {
			if _, err = rig.s.db.Exec(`UPDATE line_rollout_nodes SET after_json=? WHERE plan_id=? AND node_id=?`, raw, plan.ID, node); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := rig.s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		n, err := loadLineNode(ctx, tx, draft.Entry.NodeID, 0)
		tx.Rollback()
		if err != nil {
			t.Fatal(err)
		}
		ib := *rig.ib
		ib.Protocol = spec.Hysteria2
		line := &ForwardLine{ID: 999, Name: "Competing UDP", Backend: "relay", Protocol: "udp", TargetInboundID: ib.ID, Groups: []LineGroup{{Members: []LineMember{{ID: "new", NodeID: draft.Entry.NodeID, Port: draft.Entry.Port}}}}}
		_, _, err = compileOrdinaryLineModel(line, &ib, func(id int64) (*lineNode, error) {
			if id == n.node.ID {
				return n, nil
			}
			return &lineNode{node: rig.nodes[3], ingresses: map[int64]*Ingress{}}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "already uses udp") {
			t.Fatalf("%s reservation not enforced: %v", protocol, err)
		}
	}
}
