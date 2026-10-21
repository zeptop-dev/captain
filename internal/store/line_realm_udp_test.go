package store

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestLineRealmUDPChoosesKnownNextHopFamily(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, template := lineFixture(t)
	for _, tc := range []struct {
		name, backend, protocol, source4, source6, target4, target6, domain, want string
		shared                                                                    bool
	}{
		{name: "IPv4 source dual target", source4: "192.0.2.10", target4: "198.51.100.20", target6: "2001:db8::20", want: "198.51.100.20"},
		{name: "IPv6 source dual target", source6: "2001:db8::10", target4: "198.51.100.20", target6: "2001:db8::20", want: "2001:db8::20"},
		{name: "IPv6 only", source6: "2001:db8::10", target6: "2001:db8::20", want: "2001:db8::20"},
		{name: "dual source prefers common IPv4", source4: "192.0.2.10", source6: "2001:db8::10", target4: "198.51.100.20", target6: "2001:db8::20", want: "198.51.100.20"},
		{name: "dual source IPv6 target", source4: "192.0.2.10", source6: "2001:db8::10", target6: "2001:db8::20", want: "2001:db8::20"},
		{name: "unknown source keeps DNS", target4: "198.51.100.20", target6: "2001:db8::20", want: "target.example.com"},
		{name: "unknown target keeps DNS", source4: "192.0.2.10", want: "target.example.com"},
		{name: "no common inventory is not a routing rejection", source6: "2001:db8::10", target4: "198.51.100.20", want: "target.example.com"},
		{name: "shared DNS retains external endpoint", source4: "192.0.2.10", target4: "198.51.100.20", shared: true, want: "target.example.com"},
		{name: "UDP only", protocol: "udp", source4: "192.0.2.10", target4: "198.51.100.20", want: "198.51.100.20"},
		{name: "TCP retains DNS fallback", protocol: "tcp", source4: "192.0.2.10", target4: "198.51.100.20", want: "target.example.com"},
		{name: "relay retains DNS", backend: "relay", source4: "192.0.2.10", target4: "198.51.100.20", want: "target.example.com"},
		{name: "nft retains family-aware node DNS", backend: "nft", source4: "192.0.2.10", target4: "198.51.100.20", want: "target.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := cloneLine(template)
			l.Backend, l.Protocol, l.Groups = tc.backend, tc.protocol, l.Groups[:1]
			if l.Backend == "" {
				l.Backend = "realm"
			}
			if l.Protocol == "" {
				l.Protocol = "both"
			}
			ib.Protocol = map[string]spec.Protocol{"tcp": spec.VLESS, "udp": spec.Hysteria2, "both": spec.Shadowsocks}[l.Protocol]
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE nodes SET public_addr=?,v6_addr=?,domain='entry.example.com' WHERE id=?`, tc.source4, tc.source6, ns[0].ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE nodes SET public_addr=?,v6_addr=?,domain='target.example.com',domain_shared=? WHERE id=?`, tc.target4, tc.target6, tc.shared, ns[3].ID); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.DeleteLine(ctx, l.ID, l.Version); err != nil {
					t.Error(err)
				}
			})
			fs, err := s.NodeForwards(ctx, ns[0].ID)
			if err != nil || len(fs) != 1 || fs[0].Target != net.JoinHostPort(tc.want, "443") {
				t.Fatalf("compiled forward = %+v, %v", fs, err)
			}
			e := &domain.Entry{Name: "Published realm", LineID: &l.ID, Enabled: true}
			if err := s.CreateEntry(ctx, e); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.DeleteEntry(ctx, e.ID); err != nil {
					t.Error(err)
				}
			})
			if e.DisplayHost != "entry.example.com" {
				t.Fatalf("dial preference changed subscriber endpoint: %q", e.DisplayHost)
			}
		})
	}
}

func TestLineRealmUDPIntermediateAndExplicitIngress(t *testing.T) {
	for _, explicit := range []string{"", "mapped", "line"} {
		t.Run(explicit, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, l := lineFixture(t)
			ib.Protocol = spec.Shadowsocks
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			l.Backend, l.Protocol = "realm", "both"
			l.Groups[1].Members = l.Groups[1].Members[:1]
			if _, err := s.db.Exec(`UPDATE nodes SET domain='hop.example.com',v6_addr='2001:db8::20' WHERE id=?`, ns[1].ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE nodes SET domain='target.example.com',v6_addr='2001:db8::30' WHERE id=?`, ns[3].ID); err != nil {
				t.Fatal(err)
			}
			wantHop, wantLanding := "198.51.100.20:19443", "10.10.0.2:443"
			if explicit != "" {
				for index, item := range []struct {
					node int64
					port int
					host string
				}{{ns[1].ID, 19443, "hop-edge.example.com"}, {ns[3].ID, 443, "landing-edge.example.com"}} {
					g := &Ingress{NodeID: item.node, Name: "Explicit route", Kind: "mapped", EntryHost: item.host, PortMappings: []spec.PortMapping{{LocalFrom: item.port, LocalTo: item.port, PublicFrom: 30443}}}
					want := net.JoinHostPort(item.host, "30443")
					if explicit == "line" {
						g.LineIP = "2001:db8::40"
						want = net.JoinHostPort(g.LineIP, strconv.Itoa(item.port))
					}
					if err := s.CreateIngress(ctx, g); err != nil {
						t.Fatal(err)
					}
					if index == 0 {
						l.Groups[1].Members[0].IngressID = &g.ID
						wantHop = want
					} else {
						ib.IngressID = &g.ID
						wantLanding = want
						if err := s.UpdateInbound(ctx, ib); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			for index, want := range []string{wantHop, wantLanding} {
				fs, err := s.NodeForwards(ctx, ns[index].ID)
				if err != nil || len(fs) != 1 || fs[0].Target != want {
					t.Fatalf("hop %d = %+v, %v; want %s", index, fs, err, want)
				}
			}
		})
	}
}

func TestLineRealmUDPAddressFollowingRecompilesTarget(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, l := lineFixture(t)
	ib.Protocol = spec.Shadowsocks
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	l.Backend, l.Protocol, l.Groups = "realm", "both", l.Groups[:1]
	if _, err := s.db.Exec(`UPDATE nodes SET domain='target.example.com' WHERE id=?`, ns[3].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	before, err := s.NodeForwards(ctx, ns[0].ID)
	if err != nil || len(before) != 1 || before[0].Target != "10.10.0.2:443" {
		t.Fatalf("initial target = %+v, %v", before, err)
	}
	if _, err := s.RedeemPairCode(ctx, "pair-landing", "address-test-token", "landing", "v0.75.0", "linux"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAddressPolicy(ctx, ns[3].ID, true, false); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Second)
	for _, offset := range []time.Duration{0, 3 * time.Minute} {
		when := at.Add(offset)
		if err := s.ObserveNodeAddresses(ctx, ns[3].ID, "address-test-token", &agentproto.PublicAddresses{SampledAt: when.Unix(), IPv4: "203.0.113.30"}, when); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.ReconcileNodeAddresses(ctx, ns[3].ID, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after, err := s.NodeForwards(ctx, ns[0].ID)
	if err != nil || len(after) != 1 || after[0].Target != "203.0.113.30:443" || after[0].Tag == before[0].Tag {
		t.Fatalf("auto-follow did not update target and runtime identity: %+v, %v", after, err)
	}
	updated, err := s.LineByID(ctx, l.ID)
	if err != nil || updated.Version <= l.Version {
		t.Fatalf("line revision not updated: %+v, %v", updated, err)
	}
}

func TestLineRealmUDPDomainSharingRecompilesTarget(t *testing.T) {
	for _, position := range []string{"landing", "intermediate"} {
		t.Run(position, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, l := lineFixture(t)
			ib.Protocol = spec.Shadowsocks
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			l.Backend, l.Protocol = "realm", "both"
			l.Groups[1].Members = l.Groups[1].Members[:1]
			destination, sender, port := ns[3], ns[1], ib.Port
			if position == "intermediate" {
				destination, sender, port = ns[1], ns[0], l.Groups[1].Members[0].Port
			}
			destination.Domain = "target.example.com"
			if err := s.UpdateNode(ctx, destination); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			fs, err := s.NodeForwards(ctx, sender.ID)
			direct := net.JoinHostPort(destination.PublicAddr, strconv.Itoa(port))
			if err != nil || len(fs) != 1 || fs[0].Target != direct {
				t.Fatalf("initial target = %+v, %v; want %s", fs, err, direct)
			}
			previousTag, previousVersion := fs[0].Tag, l.Version
			for _, shared := range []bool{true, false} {
				// Only the DNS ownership flag changes; addresses and domain stay fixed.
				destination.DomainShared = shared
				if err := s.UpdateNode(ctx, destination); err != nil {
					t.Fatal(err)
				}
				want := direct
				if shared {
					want = net.JoinHostPort(destination.Domain, strconv.Itoa(port))
				}
				fs, err = s.NodeForwards(ctx, sender.ID)
				if err != nil || len(fs) != 1 || fs[0].Target != want || fs[0].Tag == previousTag {
					t.Fatalf("shared=%v target/runtime identity = %+v, %v; want %s and changed tag", shared, fs, err, want)
				}
				updated, err := s.LineByID(ctx, l.ID)
				if err != nil || updated.Version != previousVersion+1 {
					t.Fatalf("shared=%v line revision = %+v, %v; previous %d", shared, updated, err, previousVersion)
				}
				previousTag, previousVersion = fs[0].Tag, updated.Version
			}
		})
	}
}
