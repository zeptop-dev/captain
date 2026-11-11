package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"testing"
)

func TestExplicitIngressPathsCompileAndPreservePublicSubscription(t *testing.T) {
	ctx := context.Background()
	for _, backend := range []string{"relay", "realm", "nft"} {
		for _, mode := range []string{"default", "line", "public"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				s, ns, ib, l := lineFixture(t)
				g := &Ingress{NodeID: ib.NodeID, Name: "Dedicated path", Kind: "iplc", LineIP: "198.51.100.40", EntryHost: "203.0.113.40", PortMappings: []spec.PortMapping{{LocalFrom: 443, LocalTo: 443, PublicFrom: 10443}}}
				if e := s.CreateIngress(ctx, g); e != nil {
					t.Fatal(e)
				}
				ib.IngressID = &g.ID
				ib.Protocol = spec.Shadowsocks
				if e := s.UpdateInbound(ctx, ib); e != nil {
					t.Fatal(e)
				}
				l.Groups = l.Groups[:1]
				l.Backend = backend
				l.Protocol = "both"
				l.TargetAddressMode = mode
				if e := s.SaveLine(ctx, l); e != nil {
					t.Fatal(e)
				}
				want := "198.51.100.40:443"
				if mode == "public" {
					want = "203.0.113.40:10443"
				}
				fs, e := s.NodeForwards(ctx, ns[0].ID)
				if e != nil || len(fs) != 1 || fs[0].Target != want {
					t.Fatalf("%+v %v want %s", fs, e, want)
				}
				// Explicit route does not change the first group's public subscription.
				got, e := s.LineByID(ctx, l.ID)
				if e != nil || got.Endpoints[0].Host != ns[0].PublicAddr || got.Endpoints[0].Port != 18443 {
					t.Fatalf("%+v %v", got, e)
				}
			})
		}
	}
}

func TestExplicitStandalonePathRefreshAndMissingAddressRollback(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, _ := lineFixture(t)
	g := &Ingress{NodeID: ib.NodeID, Name: "Path", Kind: "iplc", LineIP: "198.51.100.40", EntryHost: "203.0.113.40", PortMappings: []spec.PortMapping{{LocalFrom: 443, LocalTo: 443, PublicFrom: 10443}}}
	if e := s.CreateIngress(ctx, g); e != nil {
		t.Fatal(e)
	}
	ib.IngressID = &g.ID
	if e := s.UpdateInbound(ctx, ib); e != nil {
		t.Fatal(e)
	}
	f := NodeForward{Forward: spec.Forward{Tag: "explicit", Port: 18443, Protocol: "tcp", Target: "example.com:1"}, InboundID: ib.ID, TargetAddressMode: "line"}
	if e := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); e != nil {
		t.Fatal(e)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "198.51.100.40:443" {
		t.Fatal(fs)
	}
	// Omitted mode in an older editor cannot silently switch the route.
	fs[0].TargetAddressMode = ""
	if e := s.SetNodeForwards(ctx, ns[0].ID, fs); e != nil {
		t.Fatal(e)
	}
	g.LineIP = "198.51.100.41"
	if e := s.UpdateIngress(ctx, g); e != nil {
		t.Fatal(e)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "198.51.100.41:443" || fs[0].TargetAddressMode != "line" {
		t.Fatal(fs)
	}
	g.LineIP = ""
	if e := s.UpdateIngress(ctx, g); e == nil {
		t.Fatal("cleared required path")
	}
	got, _ := s.IngressByID(ctx, g.ID)
	if got.LineIP != "198.51.100.41" {
		t.Fatal(got)
	}
	fs[0].TargetAddressMode = "public"
	if e := s.SetNodeForwards(ctx, ns[0].ID, fs); e != nil {
		t.Fatal(e)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "203.0.113.40:10443" {
		t.Fatal(fs)
	}
	if e := s.DeleteInbound(ctx, ib.ID); e == nil {
		t.Fatal("deleted explicit destination")
	}
}

func TestExplicitMiddleHopAndMissingPathAreAtomic(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	g := &Ingress{NodeID: ns[1].ID, Name: "Middle path", Kind: "iplc", LineIP: "198.51.100.40", EntryHost: "203.0.113.40", PortMappings: []spec.PortMapping{{LocalFrom: 19443, LocalTo: 19443, PublicFrom: 30443}}}
	if e := s.CreateIngress(ctx, g); e != nil {
		t.Fatal(e)
	}
	l.Groups[1].Members = l.Groups[1].Members[:1]
	l.Groups[1].Members[0].IngressID = &g.ID
	for _, mode := range []string{"line", "public"} {
		l.Groups[1].Members[0].AddressMode = mode
		if e := s.SaveLine(ctx, l); e != nil {
			t.Fatal(e)
		}
		fs, _ := s.NodeForwards(ctx, ns[0].ID)
		want := "198.51.100.40:19443"
		if mode == "public" {
			want = "203.0.113.40:30443"
		}
		if fs[0].Target != want {
			t.Fatal(fs)
		}
	}
	bad := cloneLine(l)
	bad.TargetAddressMode = "line"
	if e := s.SaveLine(ctx, bad); e == nil {
		t.Fatal("missing landing ingress accepted")
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "203.0.113.40:30443" {
		t.Fatal("failed edit changed active target", fs)
	}
}
