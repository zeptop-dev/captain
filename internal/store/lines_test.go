package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func lineFixture(t *testing.T, mode ...fixtureDBMode) (*Store, []*domain.Node, *domain.Inbound, *ForwardLine) {
	t.Helper()
	ctx := context.Background()
	c := fixtureDB(t, filepath.Join(t.TempDir(), "captain.db"), mode...)
	var err error
	s := New(c)
	var ns []*domain.Node
	for i, addr := range []string{"192.0.2.10", "198.51.100.20", "203.0.113.30", "10.10.0.2"} {
		n := &domain.Node{Name: []string{"entry", "relay-a", "relay-b", "landing"}[i], PublicAddr: addr}
		if err = s.CreateNode(ctx, n, "pair-"+n.Name, time.Hour); err != nil {
			t.Fatal(err)
		}
		if _, err = c.Exec(`UPDATE nodes SET version='v0.75.0' WHERE id=?`, n.ID); err != nil {
			t.Fatal(err)
		}
		ns = append(ns, n)
	}
	ib := &domain.Inbound{NodeID: ns[3].ID, Tag: "landing", Protocol: spec.VLESS, Port: 443, Enabled: true}
	if err = s.CreateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	l := &ForwardLine{Name: "Grouped transit", Protocol: "tcp", Backend: "relay", TargetInboundID: ib.ID, Groups: []LineGroup{
		{Strategy: "failover", Members: []LineMember{{NodeID: ns[0].ID, Port: 18443}}},
		{Strategy: "roundrobin", Members: []LineMember{{NodeID: ns[1].ID, Port: 19443, Weight: 3}, {NodeID: ns[2].ID, Port: 19443, Weight: 1}}},
	}}
	return s, ns, ib, l
}
func cloneLine(l *ForwardLine) *ForwardLine {
	b, _ := json.Marshal(l)
	var out ForwardLine
	_ = json.Unmarshal(b, &out)
	return &out
}

func TestLineCompilesAtomicallyAndOwnsOnlyGeneratedForwards(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, l := lineFixture(t)
	manual := NodeForward{Forward: spec.Forward{Tag: "manual", Protocol: "tcp", Port: 20443, Target: "example.com:443"}}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{manual}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if len(fs) != 2 || !reflect.DeepEqual(fs[0], manual) || fs[1].Target != "198.51.100.20:19443" || fs[1].Targets[0].Target != "203.0.113.30:19443" || fs[1].Weight != 3 || fs[1].Balance != "roundrobin" {
		t.Fatalf("wrong first layer: %+v", fs)
	}
	for _, n := range ns[1:3] {
		fs, _ := s.NodeForwards(ctx, n.ID)
		if len(fs) != 1 || fs[0].Target != "10.10.0.2:443" || fs[0].LineID != l.ID {
			t.Fatalf("wrong landing hop: %+v", fs)
		}
	}
	e := &domain.Entry{Name: "Published", LineID: &l.ID, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if e.InboundID != ib.ID || e.DisplayHost != "192.0.2.10" || e.DisplayPort != 18443 || e.LineMemberID == "" {
		t.Fatalf("entry not derived: %+v", e)
	}
	old := cloneLine(l)
	l.Groups[0].Members[0].Port = 18444
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, _ := s.EntryByID(ctx, e.ID)
	if got.DisplayPort != 18444 {
		t.Fatal("entry not updated with line", got)
	}
	if err := s.SaveLine(ctx, old); !errors.Is(err, ErrLineConflict) {
		t.Fatal("stale update", err)
	}
	before, _ := s.NodeForwards(ctx, ns[0].ID)
	bad := cloneLine(l)
	bad.Groups[0].Members[0].Port = 20443
	if err := s.SaveLine(ctx, bad); err == nil {
		t.Fatal("occupied port accepted")
	}
	after, _ := s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed edit partially changed forwards")
	}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{manual}); err != nil {
		t.Fatal(err)
	}
	after, _ = s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("old client erased owned forwards")
	}
	changed := append([]NodeForward(nil), after...)
	changed[1].Target = "example.com:9443"
	if err := s.SetNodeForwards(ctx, ns[0].ID, changed); err == nil {
		t.Fatal("old endpoint changed owned forward")
	}
	if err := s.DeleteLine(ctx, l.ID, l.Version); err == nil {
		t.Fatal("deleted referenced line")
	}
	if err := s.DeleteInbound(ctx, ib.ID); err == nil {
		t.Fatal("deleted referenced landing")
	}
	if err := s.DeleteNode(ctx, ns[1].ID); err == nil {
		t.Fatal("deleted referenced member")
	}
	if err := s.QueueNodeRemoval(ctx, "remove", ns[0].ID, NodeRemovalParams{Mode: "standalone"}); !errors.Is(err, ErrLineInUse) {
		t.Fatal("detached member", err)
	}
	if err := s.DeleteEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLine(ctx, l.ID, l.Version); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(fs, []NodeForward{manual}) {
		t.Fatal("cleanup touched manual rule", fs)
	}
}

func TestLineMappingAddressRefreshAndPublishedMemberProtection(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	g := &Ingress{NodeID: ns[0].ID, Name: "NAT", Kind: "nat", BindIP: "10.10.0.2", EntryHost: "203.0.113.30", PortMappings: []spec.PortMapping{{LocalFrom: 18443, LocalTo: 18444, PublicFrom: 30000}}}
	if err := s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].IngressID = &g.ID
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "NAT", LineID: &l.ID, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if e.DisplayPort != 30000 || e.DisplayHost != g.EntryHost {
		t.Fatal("public mapping ignored", e)
	}
	g.PortMappings[0].PublicFrom = 31000
	if err := s.UpdateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	got, _ := s.EntryByID(ctx, e.ID)
	if got.DisplayPort != 31000 {
		t.Fatal("mapping changed but published endpoint stale")
	}
	live, _ := s.LineByID(ctx, l.ID)
	if live.Version != l.Version+1 {
		t.Fatal("mapping did not revise generated forwards")
	}
	bad := cloneLine(live)
	bad.Groups[0].Members[0].ID = "replacement"
	if err := s.SaveLine(ctx, bad); err == nil {
		t.Fatal("removed published first member")
	}
	n, _ := s.NodeByID(ctx, ns[1].ID)
	n.PublicAddr = "192.0.2.10"
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "192.0.2.10:19443" {
		t.Fatal("next-hop address did not refresh", fs)
	}
	g.EntryHost = ""
	if err := s.UpdateIngress(ctx, g); err == nil {
		t.Fatal("published endpoint lost public address")
	}
	kept, _ := s.IngressByID(ctx, g.ID)
	if kept.EntryHost == "" {
		t.Fatal("failed mapping edit was not rolled back")
	}
}

func TestLineRejectsIncompatibleCandidatesAndModes(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, l := lineFixture(t)
	for _, backend := range []string{"nft", "realm"} {
		bad := cloneLine(l)
		bad.Backend = backend
		if err := s.SaveLine(ctx, bad); err == nil {
			t.Fatal("multi candidate backend accepted", backend)
		}
	}
	bad := cloneLine(l)
	bad.Groups[1].Members[0].NodeID = ns[0].ID
	if err := s.SaveLine(ctx, bad); err == nil {
		t.Fatal("node cycle accepted")
	}
	bad = cloneLine(l)
	bad.Groups[1].Members[0].NodeID = ib.NodeID
	if err := s.SaveLine(ctx, bad); err == nil {
		t.Fatal("landing reused as relay")
	}
	if _, err := s.db.Exec(`UPDATE nodes SET node_mode='monitor' WHERE id=?`, ns[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLine(ctx, l); !errors.Is(err, spec.ErrMonitorOnly) {
		t.Fatal("monitor accepted", err)
	}
	_, _ = s.db.Exec(`UPDATE nodes SET node_mode='full' WHERE id=?`, ns[1].ID)
	_, _ = s.db.Exec(`UPDATE nodes SET version='v0.48.0' WHERE id=?`, ns[0].ID)
	if err := s.SaveLine(ctx, l); err == nil {
		t.Fatal("old selecting relay accepted")
	}
	_, _ = s.db.Exec(`UPDATE nodes SET version='v0.75.0' WHERE id=?`, ns[0].ID)
	ib.Protocol = spec.Hysteria2
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	l.Protocol = "udp"
	l.Groups[1].Strategy = "failover"
	if err := s.SaveLine(ctx, l); err == nil {
		t.Fatal("UDP health failover promised")
	}
	l.Groups[1].Strategy = "roundrobin"
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	lines, _ := s.ListLines(ctx)
	if len(lines) != 1 {
		t.Fatal("failed writes left line rows", len(lines))
	}
	for _, st := range lines[0].Status {
		if st.Health != "unknown" || st.ProbeProtocol != "none" {
			t.Fatal("unmeasured UDP called healthy", st)
		}
	}
}

func TestLineMetadataPreservesRuntimeAndTransportChangesAtomically(t *testing.T) {
	s, ns, ib, l := lineFixture(t, migrateFixture)
	ctx := context.Background()
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	before, _ := s.NodeForwards(ctx, ns[0].ID)
	l.Name = "Renamed"
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	after, _ := s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("metadata edit changed runtime identity")
	}
	unrelated := &domain.Inbound{NodeID: ns[0].ID, Tag: "unrelated", Protocol: spec.VLESS, Port: 22443, Enabled: true}
	if err := s.CreateInbound(ctx, unrelated); err != nil {
		t.Fatal(err)
	}
	unrelated.Tag = "unrelated-renamed"
	if err := s.UpdateInbound(ctx, unrelated); err != nil {
		t.Fatal(err)
	}
	after, _ = s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unrelated inbound edit changed runtime identity")
	}
	ib.Protocol = spec.Hysteria2
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	current, err := s.LineByID(ctx, l.ID)
	if err != nil || current.Protocol != "udp" {
		t.Fatalf("transport did not follow: %+v %v", current, err)
	}
	after, _ = s.NodeForwards(ctx, ns[0].ID)
	if after[0].Protocol != "udp" || after[0].Tag == before[0].Tag {
		t.Fatal("changed wire path retained old runtime identity")
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: domain.RoleAdmin, Status: "active"}
	if err := s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatalf("line reset: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM forward_lines`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reset retained lines: %d %v", count, err)
	}
}

func TestLineRejectsRemovingMemberAndLanding(t *testing.T) {
	for _, role := range []string{"member", "landing"} {
		t.Run(role, func(t *testing.T) {
			s, ns, _, l := lineFixture(t)
			ctx := context.Background()
			n := ns[0]
			if role == "landing" {
				n = ns[3]
			}
			if err := s.QueueNodeRemoval(ctx, "remove", n.ID, NodeRemovalParams{Mode: "uninstall", ExpiresAt: time.Now().Add(time.Minute).Unix()}); err != nil {
				t.Fatal(err)
			}
			for _, completed := range []bool{false, true} {
				if completed {
					if _, err := s.db.Exec(`UPDATE node_jobs SET done_at=?,error='' WHERE id='remove'`, now()); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.SaveLine(ctx, cloneLine(l)); err == nil {
					t.Fatalf("removal completed=%v allowed line", completed)
				}
				fs, err := s.NodeForwards(ctx, ns[0].ID)
				if err != nil || len(fs) != 0 {
					t.Fatalf("rejected line left forwards: %v %v", fs, err)
				}
			}
			if _, err := s.db.Exec(`UPDATE node_jobs SET error='failed' WHERE id='remove'`); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatalf("failed removal blocked line: %v", err)
			}
		})
	}
}

func TestLineProtectsReverseExitLifecycle(t *testing.T) {
	for _, kind := range []string{"wireguard", "vless"} {
		t.Run(kind, func(t *testing.T) {
			s, ns, ordinary, l := lineFixture(t)
			ctx := context.Background()
			a, b := ns[3], ns[2]
			l.Groups = l.Groups[:1]
			ib := domain.Inbound{NodeID: a.ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 24443, Core: "xray", Enabled: true}
			entry := domain.Entry{Name: "reverse node", DisplayHost: "example.com", DisplayPort: 24443, Enabled: true}
			var inboundID int64
			var remove func() error
			if kind == "wireguard" {
				w := WireGuardWrite{Link: WireGuardLink{ID: "wg", TransitID: a.ID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380}, User: ib, Entry: entry}
				if err := s.SaveWireGuardLink(ctx, &w); err != nil {
					t.Fatal(err)
				}
				inboundID = w.User.ID
				remove = func() error { return s.DeleteWireGuardLink(ctx, b.ID, w.Link.ID, w.Link.Version) }
			} else {
				receiver := ib
				receiver.Tag, receiver.Port = "reverse-receiver", 25443
				writes := []ReverseWrite{{Link: ReverseLink{ID: "reverse", ExitID: b.ID, TransitID: a.ID}, User: ib, Receiver: receiver, Entry: entry}}
				if err := s.SaveReverseLinks(ctx, b.ID, map[string]int64{}, writes); err != nil {
					t.Fatal(err)
				}
				inboundID = writes[0].User.ID
				remove = func() error { return s.SaveReverseLinks(ctx, b.ID, map[string]int64{"reverse": 1}, nil) }
			}
			l.TargetInboundID = inboundID
			if err := s.QueueNodeRemoval(ctx, "remove", b.ID, NodeRemovalParams{Mode: "uninstall", ExpiresAt: time.Now().Add(time.Minute).Unix()}); err != nil {
				t.Fatal(err)
			}
			for _, completed := range []bool{false, true} {
				if completed {
					if _, err := s.db.Exec(`UPDATE node_jobs SET done_at=?,error='' WHERE id='remove'`, now()); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.SaveLine(ctx, cloneLine(l)); err == nil {
					t.Fatalf("removing reverse exit completed=%v allowed line", completed)
				}
			}
			unrelated := cloneLine(l)
			unrelated.TargetInboundID = ordinary.ID
			if err := s.SaveLine(ctx, unrelated); err != nil {
				t.Fatalf("unrelated listener on A blocked: %v", err)
			}
			if err := s.DeleteLine(ctx, unrelated.ID, unrelated.Version); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE node_jobs SET error='failed' WHERE id='remove'`); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			before, _ := s.NodeForwards(ctx, ns[0].ID)
			if used, err := s.NodeHasLines(ctx, b.ID); err != nil || !used {
				t.Fatalf("reverse exit dependencies: %v %v", used, err)
			}
			if err := s.QueueNodeRemoval(ctx, "another-remove", b.ID, NodeRemovalParams{Mode: "uninstall"}); !errors.Is(err, ErrLineInUse) {
				t.Fatalf("queued dependent exit removal: %v", err)
			}
			if err := remove(); !errors.Is(err, ErrLineInUse) {
				t.Fatalf("deleted dependent connection: %v", err)
			}
			if err := s.DeleteNode(ctx, b.ID); !errors.Is(err, ErrLineInUse) {
				t.Fatalf("deleted dependent exit: %v", err)
			}
			after, _ := s.NodeForwards(ctx, ns[0].ID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused deletion changed forward")
			}
			if err := s.DeleteLine(ctx, l.ID, l.Version); err != nil {
				t.Fatal(err)
			}
			if err := remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
