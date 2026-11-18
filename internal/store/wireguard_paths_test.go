package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"testing"
)

func TestWireGuardSharedCompletePath(t *testing.T) {
	ctx := context.Background()
	for _, protocol := range []string{"tcp", "udp", "both"} {
		t.Run(protocol, func(t *testing.T) {
			s, _, ib, route := pathTransportFixture(t, protocol, "relay", []string{"relay", "relay"})
			exit := &domain.Node{Name: "WG exit", PublicAddr: "203.0.113.30"}
			if err := s.CreateNode(ctx, exit, "pair-exit", 0); err != nil {
				t.Fatal(err)
			}
			ib.Core = "singbox"
			if ib.Protocol == spec.Hysteria2 {
				ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: "example.com", AutoCert: true}
			}
			if ib.Protocol == spec.Shadowsocks {
				ib.Settings.Cipher = "2022-blake3-aes-128-gcm"
				cfg := ib.Spec()
				spec.FillInboundSecrets(&cfg)
				ib.Settings = cfg
			}
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			link := WireGuardLink{ID: "shared-path", Name: "Path WG", TransitID: ib.NodeID, ExitID: exit.ID, Endpoint: "198.51.100.20:51820", ListenPort: 51820, MTU: 1380, Enabled: true}
			if err := s.SaveWireGuardTransport(ctx, &link); err != nil {
				t.Fatal(err)
			}
			if err := s.SetWireGuardBinding(ctx, exit.ID, link.ID, link.Version, ib.ID, true); err != nil {
				t.Fatal(err)
			}
			preview, err := s.PreviewPathRoute(ctx, route)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range preview.Dependencies {
				if d.NodeID == exit.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("WG egress omitted from path dependencies")
			}
			*route = preview.Draft
			route.PreviewRevision = preview.Revision
			if err = s.SavePathRoute(ctx, route); err != nil {
				t.Fatal(err)
			}
			referenced, err := s.NodeHasLines(ctx, exit.ID)
			if err != nil || !referenced {
				t.Fatal("exit removal protection missing", err)
			}
			if err = s.DeleteWireGuardLink(ctx, exit.ID, link.ID, link.Version+1); err == nil {
				t.Fatal("path transport deleted while referenced")
			}
			// A changed ACL cannot widen the shared transport under an existing entry.
			link.Version++
			link.PrivateAccess = &spec.PrivateAccess{Mode: "custom", Rules: []spec.PrivateAccessRule{{CIDR: "10.20.0.2/32", Protocol: "tcp", PortStart: 443}}}
			if err = s.SaveWireGuardTransport(ctx, &link); err == nil {
				t.Fatal("shared ACL silently changed")
			}
		})
	}
}
