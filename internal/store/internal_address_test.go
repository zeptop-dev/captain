package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func setInternal(t *testing.T, s *Store, n *domain.Node, addr string) {
	t.Helper()
	n.InternalAddr = addr
	if err := s.UpdateNode(context.Background(), n); err != nil {
		t.Fatal(err)
	}
}

func TestInternalLineUsesLocalPortsAndRefreshesAtomically(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, line := lineFixture(t)
	setInternal(t, s, ns[1], "10.10.0.2")
	setInternal(t, s, ns[3], "10.10.0.2")
	// NAT/public mapping must never transform an explicitly selected private hop.
	g := &Ingress{NodeID: ns[3].ID, Name: "NAT", Kind: "nat", EntryHost: "203.0.113.30", PortMappings: []spec.PortMapping{{LocalFrom: 443, LocalTo: 8443, PublicFrom: 10443}}}
	if err := s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	ib.IngressID = &g.ID
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	line.Groups[1].Members = line.Groups[1].Members[:1]
	line.Groups[1].Members[0].AddressMode = "internal"
	line.TargetAddressMode = "internal"
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "10.10.0.2:19443" {
		t.Fatal(fs)
	}
	fs, _ = s.NodeForwards(ctx, ns[1].ID)
	if fs[0].Listen != "10.10.0.2" || fs[0].Target != "10.10.0.2:443" {
		t.Fatal(fs)
	}
	entry := &domain.Entry{Name: "Published", LineID: &line.ID, Enabled: true}
	if err := s.CreateEntry(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if entry.DisplayHost != "192.0.2.10" || entry.DisplayPort != 18443 {
		t.Fatal(entry)
	}
	uses, err := s.InternalAddressUses(ctx, ns[3].ID)
	if err != nil || len(uses) != 1 || uses[0].ID != line.ID {
		t.Fatal(uses, err)
	}
	before := fs[0].Tag
	setInternal(t, s, ns[3], "2001:db8::20")
	fs, _ = s.NodeForwards(ctx, ns[1].ID)
	if fs[0].Target != "[2001:db8::20]:443" || fs[0].Tag == before {
		t.Fatal(fs)
	}
	ib.Port = 8443
	if err = s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[1].ID)
	if fs[0].Target != "[2001:db8::20]:8443" {
		t.Fatal(fs)
	}
	ns[3].InternalAddr = ""
	if err = s.UpdateNode(ctx, ns[3]); !errors.Is(err, ErrInternalAddress) {
		t.Fatal("cleared referenced address", err)
	}
	n, _ := s.NodeByID(ctx, ns[3].ID)
	if n.InternalAddr != "2001:db8::20" {
		t.Fatal("failed edit did not roll back", n.InternalAddr)
	}
	ib.Listen = "0.0.0.0"
	if err = s.UpdateInbound(ctx, ib); !errors.Is(err, ErrInternalAddress) {
		t.Fatal("incompatible bind", err)
	}
	saved, _ := s.InboundByID(ctx, ib.ID)
	if saved.Listen != "" {
		t.Fatal("failed inbound edit persisted")
	}
	g.BindIP = "192.0.2.10"
	if err = s.UpdateIngress(ctx, g); !errors.Is(err, ErrInternalAddress) {
		t.Fatal("incompatible inherited bind", err)
	}
	// Empty and null fields from old clients preserve the explicitly chosen path.
	live, _ := s.LineByID(ctx, line.ID)
	raw, _ := json.Marshal(live)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	delete(body, "target_address_mode")
	for _, group := range body["groups"].([]any) {
		for _, member := range group.(map[string]any)["members"].([]any) {
			member.(map[string]any)["address_mode"] = nil
		}
	}
	raw, _ = json.Marshal(body)
	var oldClient ForwardLine
	_ = json.Unmarshal(raw, &oldClient)
	if err = s.SaveLine(ctx, &oldClient); err != nil {
		t.Fatal(err)
	}
	if oldClient.TargetAddressMode != "internal" || oldClient.Groups[1].Members[0].AddressMode != "internal" {
		t.Fatal("old client erased address modes")
	}
	oldClient.TargetAddressMode = "default"
	oldClient.Groups[1].Members[0].AddressMode = "default"
	if err = s.SaveLine(ctx, &oldClient); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[1].ID)
	if fs[0].Target != "203.0.113.30:18443" {
		t.Fatal("explicit default did not restore mapping", fs)
	}
}

func TestInternalLineRealmUDPAndIPv6(t *testing.T) {
	for _, backend := range []string{"relay", "realm", "nft"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, l := lineFixture(t)
			setInternal(t, s, ns[1], "2001:db8::10")
			setInternal(t, s, ns[3], "2001:db8::20")
			ns[0].PublicAddr = ""
			ns[0].V6Addr = "2001:db8::30"
			if err := s.UpdateNode(ctx, ns[0]); err != nil {
				t.Fatal(err)
			}
			ib.Protocol = spec.Hysteria2
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			l.Protocol = "udp"
			l.Backend = backend
			l.Groups[1].Members = l.Groups[1].Members[:1]
			l.Groups[1].Members[0].AddressMode = "internal"
			l.TargetAddressMode = "internal"
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			fs, _ := s.NodeForwards(ctx, ns[0].ID)
			if fs[0].Target != "[2001:db8::10]:19443" {
				t.Fatal("internal hop rewritten", fs)
			}
			fs, _ = s.NodeForwards(ctx, ns[1].ID)
			if fs[0].Target != "[2001:db8::20]:443" || fs[0].Listen != "2001:db8::10" {
				t.Fatal(fs)
			}
		})
	}
}

func TestInternalAddressValidation(t *testing.T) {
	for _, raw := range []string{"", "example.com", "0.0.0.0", "::", "127.0.0.1", "::1", "169.254.1.2", "fe80::1", "fe80::1%eth0", "224.0.0.1", "ff02::1", "::ffff:127.0.0.1", "255.255.255.255"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := internalAddress(raw); err == nil {
				t.Fatal("unsafe address accepted", raw)
			}
		})
	}
	for _, raw := range []string{"10.10.0.2", "2001:db8::10", "::ffff:10.10.0.2"} {
		if _, err := internalAddress(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	s, ns, ib, l := lineFixture(t)
	ctx := context.Background()
	setInternal(t, s, ns[3], "2001:db8::10")
	ib.Protocol = spec.Snell
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	l.Protocol = "both"
	l.TargetAddressMode = "internal"
	if err := s.SaveLine(ctx, l); err == nil || !strings.Contains(err.Error(), "listener") {
		t.Fatal("IPv4-only default accepted IPv6", err)
	}
}

func TestInternalStandaloneLifecycleAndLegacyPreservation(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, _ := lineFixture(t)
	setInternal(t, s, ns[3], "10.10.0.2")
	g := &Ingress{NodeID: ib.NodeID, Name: "Mapped", Kind: "nat", EntryHost: "203.0.113.30", PortMappings: []spec.PortMapping{{LocalFrom: 443, LocalTo: 8443, PublicFrom: 10443}}}
	if err := s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	ib.IngressID = &g.ID
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	f := NodeForward{Forward: spec.Forward{Tag: "private", Port: 18443, Protocol: "tcp", Target: "203.0.113.30:10443"}, InboundID: ib.ID, TargetAddressMode: "internal"}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "10.10.0.2:443" {
		t.Fatal(fs)
	}
	f = fs[0]
	f.TargetAddressMode = ""
	f.Target = "example.com:9443"
	f.Tag = "renamed"
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "10.10.0.2:443" || fs[0].TargetAddressMode != "internal" {
		t.Fatal("old caller reverted to public", fs)
	}
	bad := fs[0]
	bad.Targets = []spec.ForwardTarget{{Target: "example.com:443"}}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{bad}); err == nil {
		t.Fatal("accepted public fallback")
	}
	bad = fs[0]
	bad.InboundID = 0
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{bad}); err == nil {
		t.Fatal("cleared private ref")
	}
	bad = fs[0]
	bad.Protocol = "udp"
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{bad}); err == nil {
		t.Fatal("nonexistent UDP listener accepted")
	}
	setInternal(t, s, ns[3], "2001:db8::20")
	ib.Port = 8443
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "[2001:db8::20]:8443" {
		t.Fatal(fs)
	}
	before := append([]NodeForward(nil), fs...)
	ib.Enabled = false
	if err := s.UpdateInbound(ctx, ib); err == nil {
		t.Fatal("disabled private target")
	}
	after, _ := s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("partial failed mutation")
	}
	for name, remove := range map[string]func() error{"node": func() error { return s.DeleteNode(ctx, ib.NodeID) }, "inbound": func() error { return s.DeleteInbound(ctx, ib.ID) }, "ingress": func() error { return s.DeleteIngress(ctx, g.ID) }, "remote": func() error {
		return s.QueueNodeRemoval(ctx, "remove", ib.NodeID, NodeRemovalParams{Mode: "uninstall", ExpiresAt: time.Now().Add(time.Minute).Unix()})
	}} {
		if err := remove(); !errors.Is(err, ErrLineInUse) {
			t.Fatal(name, err)
		}
	}
	uses, err := s.InternalAddressUses(ctx, ib.NodeID)
	if err != nil || len(uses) != 1 || uses[0].Tag != "renamed" || uses[0].SourceNodeID != ns[0].ID {
		t.Fatal(uses, err)
	}
	f = fs[0]
	f.TargetAddressMode = "default"
	f.Target = "example.com:443"
	if err = s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "example.com:443" {
		t.Fatal(fs)
	}
	ns[3].InternalAddr = ""
	if err = s.UpdateNode(ctx, ns[3]); err != nil {
		t.Fatal("unreferenced legacy address blocked", err)
	}
}

func TestInternalStandaloneProtectsReverseExitLifecycle(t *testing.T) {
	for _, kind := range []string{"wireguard", "vless"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ordinary, _ := lineFixture(t)
			a, b := ns[3], ns[2]
			setInternal(t, s, a, "10.10.0.2")
			ib := domain.Inbound{NodeID: a.ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 24443, Core: "xray", Enabled: true}
			entry := domain.Entry{Name: "Reverse", DisplayHost: "example.com", DisplayPort: 24443, Enabled: true}
			var id int64
			var remove func() error
			if kind == "wireguard" {
				w := WireGuardWrite{Link: WireGuardLink{ID: "wg", TransitID: a.ID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380}, User: ib, Entry: entry}
				if err := s.SaveWireGuardLink(ctx, &w); err != nil {
					t.Fatal(err)
				}
				id = w.User.ID
				remove = func() error { return s.DeleteWireGuardLink(ctx, b.ID, w.Link.ID, w.Link.Version) }
			} else {
				receiver := ib
				receiver.Tag, receiver.Port = "reverse-receiver", 25443
				writes := []ReverseWrite{{Link: ReverseLink{ID: "reverse", ExitID: b.ID, TransitID: a.ID}, User: ib, Receiver: receiver, Entry: entry}}
				if err := s.SaveReverseLinks(ctx, b.ID, map[string]int64{}, writes); err != nil {
					t.Fatal(err)
				}
				id = writes[0].User.ID
				remove = func() error { return s.SaveReverseLinks(ctx, b.ID, map[string]int64{"reverse": 1}, nil) }
			}
			f := NodeForward{Forward: spec.Forward{Tag: "via-reverse", Port: 18443, Protocol: "tcp"}, InboundID: id, TargetAddressMode: "internal"}
			if err := s.QueueNodeRemoval(ctx, "remove", b.ID, NodeRemovalParams{Mode: "uninstall", ExpiresAt: time.Now().Add(time.Minute).Unix()}); err != nil {
				t.Fatal(err)
			}
			for _, done := range []bool{false, true} {
				if done {
					if _, err := s.db.Exec("UPDATE node_jobs SET done_at=?,error='' WHERE id='remove'", now()); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); err == nil {
					t.Fatal("removing reverse exit accepted")
				}
			}
			unrelated := f
			unrelated.InboundID = ordinary.ID
			if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{unrelated}); err != nil {
				t.Fatal("ordinary inbound affected by reverse exit", err)
			}
			if _, err := s.db.Exec("UPDATE node_jobs SET error='failed' WHERE id='remove'"); err != nil {
				t.Fatal(err)
			}
			if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f}); err != nil {
				t.Fatal(err)
			}
			if used, err := s.NodeHasLines(ctx, b.ID); err != nil || !used {
				t.Fatal("missing reverse dependency", used, err)
			}
			uses, err := s.InternalAddressUses(ctx, b.ID)
			if err != nil || len(uses) != 0 {
				t.Fatal("exit internal address incorrectly referenced", uses, err)
			}
			for _, remove := range []func() error{remove, func() error { return s.DeleteNode(ctx, b.ID) }, func() error { return s.QueueNodeRemoval(ctx, "new-remove", b.ID, NodeRemovalParams{Mode: "uninstall"}) }} {
				if err := remove(); !errors.Is(err, ErrLineInUse) {
					t.Fatal("removed referenced reverse exit", err)
				}
			}
			if err := s.SetNodeForwards(ctx, ns[0].ID, nil); err != nil {
				t.Fatal(err)
			}
			if err := remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInternalModesPreserveMixedLegacyRulesAndMissingMemberIDs(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, line := lineFixture(t)
	setInternal(t, s, ns[3], "10.10.0.2")
	setInternal(t, s, ns[1], "10.10.0.2")
	private := NodeForward{Forward: spec.Forward{Tag: "private", Port: 18443, Protocol: "tcp"}, InboundID: ib.ID, TargetAddressMode: "internal"}
	public := NodeForward{Forward: spec.Forward{Tag: "public", Port: 18444, Protocol: "tcp", Target: "example.com:443"}, InboundID: ib.ID}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{private, public}); err != nil {
		t.Fatal(err)
	}
	private.TargetAddressMode = ""
	private.Target = "example.com:443"
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{private, public}); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[0].TargetAddressMode != "internal" || fs[1].TargetAddressMode != "" || fs[1].Target != "example.com:443" {
		t.Fatal("mixed legacy paths changed", fs)
	}
	if err := s.SetNodeForwards(ctx, ns[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	line.Groups[1].Members = line.Groups[1].Members[:1]
	line.Groups[1].Members[0].AddressMode = "internal"
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	line.Groups[1].Members[0].ID = ""
	line.Groups[1].Members[0].AddressMode = ""
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	if line.Groups[1].Members[0].AddressMode != "internal" {
		t.Fatal("missing member ID changed private path")
	}
	fs, _ = s.NodeForwards(ctx, ns[0].ID)
	if fs[0].Target != "10.10.0.2:19443" {
		t.Fatal(fs)
	}
}

func TestInternalModePreservationDoesNotOptInNewForwards(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, _ := lineFixture(t)
	setInternal(t, s, ns[3], "10.10.0.2")
	old := NodeForward{Forward: spec.Forward{Tag: "private", Port: 18443, Protocol: "tcp"}, InboundID: ib.ID, TargetAddressMode: "internal"}
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{old}); err != nil {
		t.Fatal(err)
	}
	added := old
	added.Tag = "new-default"
	added.Port++
	added.TargetAddressMode = ""
	added.Target = "example.com:443"
	old.TargetAddressMode = ""
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{old, added}); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.NodeForwards(ctx, ns[0].ID)
	if fs[1].TargetAddressMode != "" || fs[1].Target != "example.com:443" {
		t.Fatal("new legacy forward silently opted in", fs)
	}
	replacement := old
	replacement.Tag = "new-listener"
	replacement.Port = 19443
	replacement.Target = "example.com:443"
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{replacement}); !errors.Is(err, ErrInternalAddress) {
		t.Fatal("ambiguous private replacement accepted without mode", err)
	}
	replacement.TargetAddressMode = "default"
	if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{replacement}); err != nil {
		t.Fatal(err)
	}
}

func TestInternalStandaloneBackendListenFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, backend, source4, source6, target, listen, want string
		reject                                                bool
	}{
		{name: "realm IPv6", backend: "realm", source6: "2001:db8::10", target: "2001:db8::20", want: "::"},
		{name: "nft mismatch", backend: "nft", source4: "192.0.2.10", target: "2001:db8::20", reject: true},
		{name: "nft IPv6", backend: "nft", source6: "2001:db8::10", target: "2001:db8::20", want: "::"},
		{name: "explicit bind", backend: "nft", source6: "2001:db8::10", target: "10.10.0.2", listen: "0.0.0.0", want: "0.0.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, _ := lineFixture(t)
			ns[0].PublicAddr, ns[0].V6Addr = tc.source4, tc.source6
			if err := s.UpdateNode(ctx, ns[0]); err != nil {
				t.Fatal(err)
			}
			setInternal(t, s, ns[3], tc.target)
			f := NodeForward{Forward: spec.Forward{Tag: "private", Port: 18443, Protocol: "tcp", Backend: tc.backend, Listen: tc.listen}, InboundID: ib.ID, TargetAddressMode: "internal"}
			err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{f})
			if tc.reject {
				if err == nil {
					t.Fatal("backend family mismatch accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fs, _ := s.NodeForwards(ctx, ns[0].ID)
			if fs[0].Listen != tc.want {
				t.Fatal(fs)
			}
		})
	}
}

func TestInternalAddressEditLeavesDefaultLineRuntimeUnchanged(t *testing.T) {
	ctx := context.Background()
	s, ns, _, line := lineFixture(t)
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	before, _ := s.NodeForwards(ctx, ns[1].ID)
	setInternal(t, s, ns[3], "10.10.0.2")
	after, _ := s.NodeForwards(ctx, ns[1].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("default path changed after address edit", before, after)
	}
}
