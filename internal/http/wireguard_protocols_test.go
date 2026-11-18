package http

import (
	"context"
	"encoding/json"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"strings"
	"testing"
	"time"
)

func TestWireGuardEntryProtocolSelection(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	transit, err := r.st.NodeByID(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	transit.PublicAddr = "198.51.100.20"
	if err := r.st.UpdateNode(ctx, transit); err != nil {
		t.Fatal(err)
	}
	created := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "WG options exit", "PublicAddr": "203.0.113.30"}, 200))
	exitID := int64(created["id"].(float64))
	if err := r.st.ResetPairCode(ctx, exitID, "wg-options-pair", time.Minute); err != nil {
		t.Fatal(err)
	}
	const exitToken = "wg-options-exit-token"
	if _, err := r.st.RedeemPairCode(ctx, "wg-options-pair", auth.SHA256Hex(exitToken), "test", "v0.74.0", "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	exitAgent := &client{t: t, srv: r.srv, token: exitToken}
	cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{WireGuardReverse: true, PrivateAccess: true, Protocols: []spec.Protocol{spec.VLESS, spec.VMess, spec.Trojan, spec.Shadowsocks}}}}
	for _, name := range []string{"xray", "singbox", "singbox-extended"} {
		cap := spec.CapabilitiesForCore(name)
		cores[name] = agentproto.CoreStatus{Capabilities: &cap}
	}
	for _, id := range []int64{r.nodeID, exitID} {
		if err := r.st.TouchNode(ctx, id, "v0.74.0", "", spec.SystemStatus{}, cores, nil); err != nil {
			t.Fatal(err)
		}
	}
	report := func(id int64, rep *agentproto.WireGuardReport) {
		t.Helper()
		token := exitToken
		if id == r.nodeID {
			token = r.agent.token
		}
		if err := r.st.SetWireGuardReport(ctx, id, auth.SHA256Hex(token), rep); err != nil {
			t.Fatal(err)
		}
	}

	_ = exitAgent
	report(r.nodeID, &agentproto.WireGuardReport{Available: true})
	report(exitID, &agentproto.WireGuardReport{Available: true})
	path := "/api/admin/settings/nodes/" + itoa(exitID) + "/wireguard-connections"
	input := map[string]any{"transit_id": r.nodeID, "name": "Protocol entry", "port": 19443, "tunnel_port": 51821, "endpoint": "198.51.100.20:51821", "mtu": 1380, "enabled": true, "protocol": "vless", "settings": map[string]any{"flow": "xtls-rprx-vision", "tls": map[string]any{"mode": 1, "server_name": "node.example.com", "auto_cert": true, "acme": "http"}}}
	response := mustJSON[map[string]any](t, call(r.c, "POST", path, input, 200))
	id := response["id"].(string)
	for _, proto := range []spec.Protocol{spec.VLESS, spec.VMess, spec.Trojan, spec.Shadowsocks, spec.Hysteria2, spec.TUIC, spec.AnyTLS, spec.Naive, spec.Snell, spec.SOCKS, spec.HTTP, spec.SSH, spec.Mieru, spec.WireGuard} {
		links, err := r.st.WireGuardLinks(ctx, exitID)
		if err != nil || len(links) != 1 {
			t.Fatal(err)
		}
		link := links[0]
		input["version"] = link.Version
		input["protocol"] = proto
		selectedCore := "xray"
		switch proto {
		case spec.VLESS, spec.VMess, spec.Trojan, spec.Shadowsocks, spec.WireGuard:
		case spec.SSH, spec.Mieru:
			selectedCore = "singbox-extended"
		default:
			selectedCore = "singbox"
		}
		input["core"] = selectedCore
		settings := map[string]any{"tls": map[string]any{"mode": 1, "server_name": "node.example.com", "auto_cert": true, "acme": "http"}}
		if proto == spec.Shadowsocks {
			settings = map[string]any{"cipher": "aes-128-gcm"}
		}
		switch proto {
		case spec.WireGuard:
			settings = map[string]any{"wg_mtu": 1280, "wg_address": "10.66.0.1/16"}
		case spec.Snell:
			settings = map[string]any{"snell_version": 5, "snell_multi_user": true}
		case spec.SOCKS, spec.HTTP, spec.SSH, spec.Mieru:
			settings = map[string]any{}
		}
		input["settings"] = settings
		call(r.c, "PUT", path+"/"+id, input, 200)
		got, _ := r.st.InboundByID(ctx, link.UserInboundID)
		if got.Protocol != proto || got.Core != selectedCore || got.Settings.WireGuardSlot != link.Slot {
			t.Fatalf("wrong saved protocol: %#v", got)
		}
		if settings["tls"] != nil && (got.Settings.TLS.Mode != spec.TLSStandard || got.Settings.TLS.Reality != nil) {
			t.Fatal("TLS replaced with REALITY")
		}
		node, _ := r.st.NodeByID(ctx, r.nodeID)
		state, e := (&service.AgentState{Store: r.st}).Build(ctx, node, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, ib := range state.Node.Inbounds {
			if ib.Tag == got.Tag {
				found = true
				if ib.Protocol != proto {
					t.Fatal("state protocol lost")
				}
			}
		}
		if !found {
			t.Fatal("selected protocol omitted")
		}
		supported := cores[selectedCore]
		downgraded := *supported.Capabilities
		downgraded.WireGuardReverse = false
		cores[selectedCore] = agentproto.CoreStatus{Capabilities: &downgraded}
		if e := r.st.TouchNode(ctx, r.nodeID, "test", "", spec.SystemStatus{}, cores, nil); e != nil {
			t.Fatal(e)
		}
		denied, e := (&service.AgentState{Store: r.st}).Build(ctx, node, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range denied.Node.Inbounds {
			if entry.Tag == got.Tag {
				t.Fatal("unsupported core can receive WG inbound")
			}
		}
		cores[selectedCore] = supported
		if e := r.st.TouchNode(ctx, r.nodeID, "test", "", spec.SystemStatus{}, cores, nil); e != nil {
			t.Fatal(e)
		}

		views := mustJSON[[]map[string]any](t, call(r.c, "GET", path, nil, 200))
		if views[0]["protocol"] != string(proto) {
			t.Fatal("editor protocol missing")
		}
		if _, ok := views[0]["settings"].(map[string]any); !ok {
			t.Fatal("settings missing")
		}
		if len(views) > 0 {
			raw, _ := json.Marshal(views)
			if strings.Contains(string(raw), link.TransitKey) || strings.Contains(string(raw), link.ExitKey) {
				t.Fatal("WG key leaked")
			}
		}
		if proto == spec.Snell {
			current, _ := r.st.WireGuardLinks(ctx, exitID)
			input["version"] = current[0].Version
			input["settings"] = map[string]any{"snell_version": 5, "snell_multi_user": false}
			call(r.c, "PUT", path+"/"+id, input, 409)
			unchanged, _ := r.st.WireGuardLinks(ctx, exitID)
			if unchanged[0].Version != current[0].Version {
				t.Fatal("rejected shared PSK changed connection")
			}
			input["settings"] = settings
		}
		// Omitted additive fields preserve the current protocol/settings.
		links, _ = r.st.WireGuardLinks(ctx, exitID)
		input["version"] = links[0].Version
		delete(input, "settings")
		delete(input, "protocol")
		call(r.c, "PUT", path+"/"+id, input, 200)
		got, _ = r.st.InboundByID(ctx, link.UserInboundID)
		if got.Protocol != proto {
			t.Fatal("legacy edit reset protocol")
		}
	}
	links, _ := r.st.WireGuardLinks(ctx, exitID)
	saved, _ := r.st.InboundByID(ctx, links[0].UserInboundID)
	input["version"] = links[0].Version
	input["protocol"] = "trojan"
	call(r.c, "PUT", path+"/"+id, input, 400)
	after, _ := r.st.InboundByID(ctx, links[0].UserInboundID)
	if after.Protocol != saved.Protocol {
		t.Fatal("failed update mutated protocol")
	}
	// Independent transport is created without subscriber or entry ownership.
	independent := map[string]any{"independent": true, "transit_id": r.nodeID, "name": "Shared transport", "tunnel_port": 51822, "endpoint": "198.51.100.20:51822", "mtu": 1380, "enabled": true}
	createdTransport := mustJSON[map[string]any](t, call(r.c, "POST", path, independent, 200))
	transportID := createdTransport["id"].(string)
	transportPath := path + "/" + transportID
	var boundIDs []int64
	for index, proto := range []spec.Protocol{spec.VLESS, spec.SOCKS} {
		coreName := "xray"
		if proto == spec.SOCKS {
			coreName = "singbox"
		}
		ib := &domain.Inbound{NodeID: r.nodeID, Tag: "shared-" + string(proto), Protocol: proto, Core: coreName, Port: 19500 + index, Enabled: true}
		if proto == spec.SOCKS {
			ib.Settings.Standalone = &spec.StandaloneAuth{Username: "independent", Password: "fixture-password"}
		}
		if err := r.st.CreateInbound(ctx, ib); err != nil {
			t.Fatal(err)
		}
		boundIDs = append(boundIDs, ib.ID)
		call(r.c, "PUT", transportPath+"/bindings", map[string]any{"version": index + 1, "inbound_id": ib.ID, "attach": true}, 200)
	}
	shared, _ := r.st.WireGuardBindings(ctx, transportID)
	if len(shared) != 2 {
		t.Fatal("binding lost")
	}
	for _, ib := range shared {
		if ib.Settings.WireGuardSlot == 0 {
			t.Fatal("routing not bound")
		}
	}
	// Optimistic concurrency, deletion protection and existing-port edits.
	call(r.c, "PUT", transportPath+"/bindings", map[string]any{"version": 1, "inbound_id": boundIDs[0], "attach": false}, 409)
	call(r.c, "DELETE", transportPath, map[string]any{"version": 3}, 409)
	call(r.c, "DELETE", "/api/admin/nodes/"+itoa(exitID), nil, 409)
	independent["version"] = 3
	independent["mtu"] = 1360
	call(r.c, "PUT", transportPath, independent, 200)
	views := mustJSON[[]map[string]any](t, call(r.c, "GET", path, nil, 200))
	if len(views) != 2 {
		t.Fatal("independent transport disappeared")
	}
	node, _ := r.st.NodeByID(ctx, r.nodeID)
	state, err := (&service.AgentState{Store: r.st}).Build(ctx, node, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Node.WireGuardLinks) != 2 {
		t.Fatal("shared transport was duplicated or withheld")
	}
	for index, id := range boundIDs {
		call(r.c, "PUT", transportPath+"/bindings", map[string]any{"version": 4 + index, "inbound_id": id, "attach": false}, 200)
		ib, _ := r.st.InboundByID(ctx, id)
		if ib.Enabled || ib.Settings.WireGuardSlot != 0 {
			t.Fatal("detach enabled an unintended direct route")
		}
	}
	call(r.c, "DELETE", transportPath, map[string]any{"version": 6}, 200)
	for _, id := range boundIDs {
		if _, err := r.st.InboundByID(ctx, id); err != nil {
			t.Fatal("transport deleted borrowed inbound")
		}
	}

}
