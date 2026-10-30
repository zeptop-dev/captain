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

func futurePathLanding(t *testing.T, mode ...string) (*rolloutRig, *LineRollout, *domain.Inbound) {
	t.Helper()
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	p = r.drain(t, p.ID)
	node := &domain.Node{Name: "future landing", PublicAddr: "192.0.2.50"}
	if len(mode) > 0 && mode[0] == "internal" {
		node.InternalAddr = "10.10.0.20"
	}
	if len(mode) > 0 && mode[0] == "domain" {
		node.Domain = "landing.example.com"
		node.DomainShared = true
	}
	if e = r.s.CreateNode(ctx, node, "future-pair", time.Hour); e != nil {
		t.Fatal(e)
	}
	if _, e = r.s.db.Exec(`UPDATE nodes SET token_hash=?,version='v0.84.0' WHERE id=?`, r.token(node.ID), node.ID); e != nil {
		t.Fatal(e)
	}
	delivery, e := r.s.PrepareNodeConfigDelivery(ctx, node.ID, r.token(node.ID), configSessionA, "future", "future", r.at)
	if e != nil {
		t.Fatal(e)
	}
	r.receipt(t, node.ID, delivery, "future", "future", true)
	ib := &domain.Inbound{NodeID: node.ID, Tag: "future", Protocol: spec.VLESS, Port: 8443, Enabled: true, Settings: spec.Inbound{TLS: &spec.TLS{Mode: spec.TLSStandard, ServerName: "example.com"}}}
	if e = r.s.CreateInbound(ctx, ib); e != nil {
		t.Fatal(e)
	}
	next, e := r.s.PathRoute(ctx, p.LineID)
	if e != nil {
		t.Fatal(e)
	}
	next.TargetInboundID = ib.ID
	if len(mode) > 0 && mode[0] == "internal" {
		next.TargetAddressMode = AddressModeInternal
	}
	if len(mode) > 0 && mode[0] == "ingress" {
		g := &Ingress{NodeID: node.ID, Name: "future mapped landing", BindIP: "127.0.0.1", LineIP: "192.0.2.70", EntryHost: "192.0.2.71", PortFrom: 8000, PortTo: 8999, PortOffset: 1000}
		if e = r.s.CreateIngress(ctx, g); e != nil {
			t.Fatal(e)
		}
		ib.IngressID = &g.ID
		if e = r.s.UpdateInbound(ctx, ib); e != nil {
			t.Fatal(e)
		}
	}
	next.Entry.Port += 1000
	for i := range next.Branches {
		for j := range next.Branches[i].Ports {
			next.Branches[i].Ports[j] += 1000
		}
	}
	p, e = r.s.PlanPathRollout(ctx, next, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return r, p, ib
}

func TestPathRolloutFutureLandingStructuralMutationGuard(t *testing.T) {
	cases := map[string]func(*domain.Inbound){
		"port":         func(i *domain.Inbound) { i.Port++ },
		"listen":       func(i *domain.Inbound) { i.Listen = "127.0.0.1" },
		"disabled":     func(i *domain.Inbound) { i.Enabled = false },
		"protocol":     func(i *domain.Inbound) { i.Protocol = spec.HTTP },
		"core":         func(i *domain.Inbound) { i.Core = "xray" },
		"tls identity": func(i *domain.Inbound) { i.Settings.TLS.ServerName = "new.example.com" },
		"tls alpn":     func(i *domain.Inbound) { i.Settings.TLS.ALPN = []string{"h2"} },
		"transport":    func(i *domain.Inbound) { i.Settings.Transport = &spec.Transport{Type: "ws", Path: "/changed"} },
		"proxy prefix": func(i *domain.Inbound) { i.Settings.AcceptProxyProtocol = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, p, ib := futurePathLanding(t)
			ctx := context.Background()
			if name == "disabled" {
				var err error
				p, err = r.s.PauseLineRollout(ctx, p.ID, p.Version)
				if err != nil {
					t.Fatal(err)
				}
			}
			mutate(ib)
			if err := r.s.UpdateInbound(ctx, ib); !errors.Is(err, ErrLineRolloutConflict) {
				t.Fatalf("future landing mutation accepted: %v", err)
			}
			stored, err := r.s.InboundByID(ctx, ib.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Port != 8443 || !stored.Enabled || stored.Settings.TLS.ServerName != "example.com" {
				t.Fatal("rejected edit leaked", stored)
			}
		})
	}
}

func TestPathRolloutFutureLandingDeleteAndLiveCredentials(t *testing.T) {
	r, p, ib := futurePathLanding(t)
	ctx := context.Background()
	if err := r.s.DeleteInbound(ctx, ib.ID); !errors.Is(err, ErrLineInUse) {
		t.Fatal("future inbound removed", err)
	}
	if err := r.s.DeleteNode(ctx, ib.NodeID); !errors.Is(err, ErrLineInUse) {
		t.Fatal("future node removed", err)
	}
	ib.Settings.ScopedUsers = true
	ib.Settings.Users = []spec.User{{ID: 42, Name: "test", Password: "synthetic-rotated"}}
	ib.Settings.TLS.CertPath = "renewed.crt"
	ib.Settings.TLS.KeyPath = "renewed.key"
	ib.Sort = 12
	if err := r.s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal("current credentials blocked", err)
	}
	stored, err := r.s.InboundByID(ctx, ib.ID)
	if err != nil || !stored.Settings.ScopedUsers || len(stored.Settings.Users) != 1 || stored.Settings.TLS.CertPath != "renewed.crt" {
		t.Fatal("credentials not current", stored, err)
	}
	if got := r.read(t, p.ID); got.Version != p.Version || got.Status != "running" {
		t.Fatal("credential edit changed release", got)
	}
	unrelated := &domain.Inbound{NodeID: ib.NodeID, Tag: "unrelated", Protocol: spec.HTTP, Port: 10443, Enabled: true}
	if err = r.s.CreateInbound(ctx, unrelated); err != nil {
		t.Fatal(err)
	}
	unrelated.Port++
	if err = r.s.UpdateInbound(ctx, unrelated); err != nil {
		t.Fatal("unrelated listener blocked", err)
	}
}

func TestPathRolloutFutureLandingAddressTransactionGuard(t *testing.T) {
	for _, mode := range []string{"public", "internal", "domain"} {
		t.Run(mode, func(t *testing.T) {
			r, p, ib := futurePathLanding(t, mode)
			ctx := context.Background()
			n, err := r.s.NodeByID(ctx, ib.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			before := *n
			switch mode {
			case "public":
				n.PublicAddr = "192.0.2.51"
			case "internal":
				n.InternalAddr = "10.10.0.21"
			case "domain":
				n.Domain = "changed.example.com"
			}
			if err = r.s.UpdateNode(ctx, n); !errors.Is(err, ErrLineRolloutConflict) {
				t.Fatal("future address changed", err)
			}
			stored, err := r.s.NodeByID(ctx, n.ID)
			if err != nil || stored.PublicAddr != before.PublicAddr || stored.InternalAddr != before.InternalAddr || stored.Domain != before.Domain {
				t.Fatal("failed address update leaked", stored, err)
			}
			stored.Name = "metadata allowed"
			if err = r.s.UpdateNode(ctx, stored); err != nil {
				t.Fatal("metadata blocked", err)
			}
			if r.read(t, p.ID).Version != p.Version {
				t.Fatal("failed change modified plan")
			}
		})
	}
}
func TestPathRolloutFutureLandingIngressGuard(t *testing.T) {
	for _, kind := range []string{"bind", "target", "mapping", "delete"} {
		t.Run(kind, func(t *testing.T) {
			r, _, ib := futurePathLanding(t, "ingress")
			ctx := context.Background()
			g, err := r.s.IngressByID(ctx, *ib.IngressID)
			if err != nil {
				t.Fatal(err)
			}
			before := *g
			if kind == "delete" {
				if err = r.s.DeleteIngress(ctx, g.ID); !errors.Is(err, ErrLineInUse) {
					t.Fatal("future ingress deleted", err)
				}
				return
			}
			switch kind {
			case "bind":
				g.BindIP = "127.0.0.2"
			case "target":
				g.LineIP = "192.0.2.72"
			case "mapping":
				g.PortOffset++
			}
			if err = r.s.UpdateIngress(ctx, g); !errors.Is(err, ErrLineRolloutConflict) {
				t.Fatal("future ingress changed", err)
			}
			stored, err := r.s.IngressByID(ctx, g.ID)
			if err != nil || stored.BindIP != before.BindIP || stored.LineIP != before.LineIP || stored.PortOffset != before.PortOffset {
				t.Fatal("failed ingress update leaked", stored, err)
			}
			stored.Name = "metadata allowed"
			if err = r.s.UpdateIngress(ctx, stored); err != nil {
				t.Fatal("ingress metadata blocked", err)
			}
		})
	}
}

func TestPathRolloutFutureLandingAutomaticAddressGuard(t *testing.T) {
	r, _, ib := futurePathLanding(t)
	ctx := context.Background()
	if err := r.s.SetAddressPolicy(ctx, ib.NodeID, true, false); err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{5 * time.Minute, 7 * time.Minute} {
		at := r.at.Add(elapsed)
		if err := r.s.ObserveNodeAddresses(ctx, ib.NodeID, r.token(ib.NodeID), &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: "192.0.2.51"}, at); err != nil {
			t.Fatal("resource observation blocked", err)
		}
	}
	if _, _, err := r.s.ReconcileNodeAddresses(ctx, ib.NodeID, r.at.Add(7*time.Minute)); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("auto-follow changed frozen future endpoint", err)
	}
	n, err := r.s.NodeByID(ctx, ib.NodeID)
	if err != nil || n.PublicAddr != "192.0.2.50" {
		t.Fatal("auto-follow failure leaked", n, err)
	}
}
