package store

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestLineBackendAddressFamilies(t *testing.T) {
	ctx := context.Background()
	s, ns, _, template := lineFixture(t)
	for _, tc := range []struct {
		name, backend, public, v6, domain, target, listen, wantError string
		ingress                                                      *Ingress
	}{
		{name: "nft IPv4 to IPv6", backend: "nft", public: "192.0.2.10", target: "2001:db8::20", wantError: "same address family"},
		{name: "nft IPv6 to IPv4", backend: "nft", v6: "2001:db8::10", target: "198.51.100.20", wantError: "same address family"},
		{name: "nft IPv6", backend: "nft", v6: "2001:db8::10", target: "2001:db8::20", listen: "::"},
		{name: "nft IPv4", backend: "nft", public: "192.0.2.10", target: "198.51.100.20", listen: "0.0.0.0"},
		{name: "nft target domain resolved by node in listen family", backend: "nft", v6: "2001:db8::10", target: "target.example.com", listen: "::"},
		{name: "nft unknown entry domain is not resolved in transaction", backend: "nft", domain: "entry.example.com", target: "2001:db8::20"},
		{name: "realm IPv6", backend: "realm", v6: "2001:db8::10", target: "198.51.100.20", listen: "::"},
		{name: "realm dual stack domain", backend: "realm", public: "192.0.2.10", v6: "2001:db8::10", domain: "entry.example.com", target: "198.51.100.20", listen: "::"},
		{name: "realm IPv4", backend: "realm", public: "192.0.2.10", target: "198.51.100.20"},
		{name: "realm IPv4 listener with IPv6 target", backend: "realm", public: "192.0.2.10", target: "2001:db8::20"},
		{name: "relay IPv6 remains dual stack", backend: "relay", v6: "2001:db8::10", target: "198.51.100.20"},
		{name: "nft actual local bind overrides provider endpoint family", backend: "nft", target: "198.51.100.20", listen: "10.10.0.2", ingress: &Ingress{BindIP: "10.10.0.2", EntryHost: "2001:db8::10"}},
		{name: "realm actual local bind overrides public inventory", backend: "realm", v6: "2001:db8::10", target: "198.51.100.20", listen: "10.10.0.2", ingress: &Ingress{BindIP: "10.10.0.2", EntryHost: "203.0.113.30"}},
		{name: "nft ambiguous mapped families need local bind", backend: "nft", target: "198.51.100.20", wantError: "local bind", ingress: &Ingress{LineIP: "192.0.2.10", EntryHost: "2001:db8::10"}},
		{name: "realm mapped IPv6 endpoint", backend: "realm", target: "198.51.100.20", listen: "::", ingress: &Ingress{LineIP: "192.0.2.10", EntryHost: "2001:db8::10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := cloneLine(template)
			if _, err := s.db.Exec(`UPDATE nodes SET public_addr=?,v6_addr=?,domain=? WHERE id=?`, tc.public, tc.v6, tc.domain, ns[0].ID); err != nil {
				t.Fatal(err)
			}
			targetPublic, targetV6, targetDomain := "", "", ""
			if ip := net.ParseIP(tc.target); ip == nil {
				targetDomain = tc.target
			} else if ip.To4() != nil {
				targetPublic = tc.target
			} else {
				targetV6 = tc.target
			}
			if _, err := s.db.Exec(`UPDATE nodes SET public_addr=?,v6_addr=?,domain=? WHERE id=?`, targetPublic, targetV6, targetDomain, ns[3].ID); err != nil {
				t.Fatal(err)
			}
			line.Backend, line.Groups = tc.backend, line.Groups[:1]
			if tc.ingress != nil {
				g := *tc.ingress
				g.NodeID, g.Name, g.Kind = ns[0].ID, "Mapped entry", "mapped"
				g.PortMappings = []spec.PortMapping{{LocalFrom: 18443, LocalTo: 18443, PublicFrom: 30443}}
				if err := s.CreateIngress(ctx, &g); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := s.DeleteIngress(ctx, g.ID); err != nil {
						t.Error(err)
					}
				})
				line.Groups[0].Members[0].IngressID = &g.ID
			}
			err := s.SaveLine(ctx, line)
			if err == nil {
				t.Cleanup(func() {
					if err := s.DeleteLine(ctx, line.ID, line.Version); err != nil {
						t.Error(err)
					}
				})
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("SaveLine error = %v, want %q", err, tc.wantError)
				}
				for _, n := range ns {
					forwards, err := s.NodeForwards(ctx, n.ID)
					if err != nil || len(forwards) != 0 {
						t.Fatalf("failed save left forwards: %+v, %v", forwards, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			forwards, err := s.NodeForwards(ctx, ns[0].ID)
			if err != nil || len(forwards) != 1 || forwards[0].Listen != tc.listen {
				t.Fatalf("forwards = %+v, %v; want listen %q", forwards, err, tc.listen)
			}
			if tc.ingress != nil {
				e := &domain.Entry{Name: "Mapped subscriber", LineID: &line.ID, Enabled: true}
				if err := s.CreateEntry(ctx, e); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := s.DeleteEntry(ctx, e.ID); err != nil {
						t.Error(err)
					}
				})
				if e.DisplayHost != tc.ingress.EntryHost || e.DisplayPort != 30443 {
					t.Fatalf("wildcard listen changed published mapping: %+v", e)
				}
			}
		})
	}
}

func TestLineBackendFamiliesApplyToIntermediateMembers(t *testing.T) {
	for _, backend := range []string{"realm", "nft"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			s, ns, _, line := lineFixture(t)
			line.Backend = backend
			line.Groups[1].Members = line.Groups[1].Members[:1]
			if _, err := s.db.Exec(`UPDATE nodes SET public_addr='',v6_addr='2001:db8::20' WHERE id=?`, ns[1].ID); err != nil {
				t.Fatal(err)
			}
			err := s.SaveLine(ctx, line)
			if backend == "nft" {
				if err == nil || !strings.Contains(err.Error(), "same address family") {
					t.Fatalf("IPv4 to IPv6 intermediate nft member accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			forwards, err := s.NodeForwards(ctx, ns[1].ID)
			if err != nil || len(forwards) != 1 || forwards[0].Listen != "::" {
				t.Fatalf("IPv6 intermediate realm listener = %+v, %v", forwards, err)
			}
		})
	}
}

func TestLineNFTAddressFamilyRefreshRollsBack(t *testing.T) {
	ctx := context.Background()
	s, ns, _, line := lineFixture(t)
	line.Backend = "nft"
	line.Groups[1].Members = line.Groups[1].Members[:1]
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "Published nft", LineID: &line.ID, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	before, err := s.NodeForwards(ctx, ns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.NodeByID(ctx, ns[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	n.PublicAddr, n.V6Addr = "", "2001:db8::20"
	if err := s.UpdateNode(ctx, n); err == nil || !strings.Contains(err.Error(), "same address family") {
		t.Fatalf("address update error = %v", err)
	}
	gotNode, err := s.NodeByID(ctx, n.ID)
	if err != nil || gotNode.PublicAddr != ns[1].PublicAddr || gotNode.V6Addr != "" {
		t.Fatalf("failed address update was not rolled back: %+v, %v", gotNode, err)
	}
	after, err := s.NodeForwards(ctx, ns[0].ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed address update changed compiled rules: %+v, %v", after, err)
	}
	gotLine, err := s.LineByID(ctx, line.ID)
	if err != nil || gotLine.Version != line.Version {
		t.Fatalf("failed address update changed line version: %+v, %v", gotLine, err)
	}
	gotEntry, err := s.EntryByID(ctx, e.ID)
	if err != nil || gotEntry.DisplayHost != e.DisplayHost || gotEntry.DisplayPort != e.DisplayPort {
		t.Fatalf("failed address update changed published endpoint: %+v, %v", gotEntry, err)
	}
}
