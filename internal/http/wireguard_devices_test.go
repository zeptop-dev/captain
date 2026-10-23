package http

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/bosun/pkg/wg"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func nativeWGFixture(t *testing.T) (*rig, *domain.User, domain.Inbound, domain.Entry) {
	t.Helper()
	r := newRig(t)
	ctx := context.Background()
	uid, _ := r.user("wg-devices@example.com")
	u, err := r.st.UserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	private, public, err := wg.Keypair()
	if err != nil {
		t.Fatal(err)
	}
	ib := domain.Inbound{NodeID: r.nodeID, Tag: "native-wg", Protocol: spec.WireGuard, Port: 51820, Core: "xray", Enabled: true, Settings: spec.Inbound{WGPrivateKey: private, WGPublicKey: public}}
	if err = r.st.CreateInbound(ctx, &ib); err != nil {
		t.Fatal(err)
	}
	entry := domain.Entry{Name: "Native WG", InboundID: ib.ID, DisplayHost: "wg.example.com", DisplayPort: 51820, Enabled: true}
	if err = r.st.CreateEntry(ctx, &entry); err != nil {
		t.Fatal(err)
	}
	nativeWGCapability(t, r, "v0.77.0", true)
	return r, u, ib, entry
}
func nativeWGCapability(t *testing.T, r *rig, version string, support bool) {
	t.Helper()
	cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{WireGuardDevices: support}}}
	if err := r.st.TouchNode(context.Background(), r.nodeID, version, "", spec.SystemStatus{}, cores, nil); err != nil {
		t.Fatal(err)
	}
}
func createNativeDevice(t *testing.T, r *rig, uid int64, name string) (service.WireGuardDeviceView, string) {
	t.Helper()
	code, b, h := r.c.do("POST", "/api/admin/users/"+itoa(uid)+"/wireguard-devices", map[string]string{"name": name}, nil)
	if code != 200 {
		t.Fatalf("register: %d %s", code, b)
	}
	if h.Get("Cache-Control") != "no-store" {
		t.Fatal("credentials cacheable")
	}
	d := mustJSON[service.WireGuardDeviceView](t, b)
	parsed, err := url.Parse(d.SubscriptionURL)
	if err != nil {
		t.Fatal(err)
	}
	return d, parsed.Path
}
func nativeState(t *testing.T, r *rig) *agentproto.State {
	t.Helper()
	n, err := r.st.NodeByID(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	st, err := (&service.AgentState{Store: r.st}).Build(context.Background(), n, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return st
}
func devicePeerCount(st *agentproto.State, tag string, uid int64) int {
	for _, ib := range st.Node.Inbounds {
		if ib.Tag == tag {
			for _, u := range ib.EffectiveUsers(st.Users) {
				if u.ID == uid {
					return len(u.WireGuardPeers)
				}
			}
		}
	}
	return 0
}

func TestNativeWireGuardDeviceCredentialScopeAndState(t *testing.T) {
	r, u, ib, e := nativeWGFixture(t)
	ctx := context.Background()
	inbounds, _ := r.st.InboundsByNode(ctx, r.nodeID)
	ordinary := domain.Entry{Name: "Ordinary secret", InboundID: inbounds[0].ID, DisplayHost: "ordinary.example.com", DisplayPort: 443, Enabled: true}
	if err := r.st.CreateEntry(ctx, &ordinary); err != nil {
		t.Fatal(err)
	}
	d, path := createNativeDevice(t, r, u.ID, "Phone")
	_, otherPath := createNativeDevice(t, r, u.ID, "Laptop")
	row, err := r.st.WireGuardDeviceByToken(ctx, strings.TrimPrefix(path, "/sub/"))
	if err != nil {
		t.Fatal(err)
	}
	expectedPrivate, _, _ := wg.DeriveDevicePeer(ib.Settings.WGPrivateKey, u.UUID, row.UUID)
	legacy := subscription.WGConf(subscription.Line{Inbound: ib.Spec(), UUID: u.UUID, UserID: u.AgentID, Host: e.DisplayHost, Port: e.DisplayPort})
	code, body, _ := r.c.do("GET", path+"?client=wireguard", nil, nil)
	if code != 200 || !strings.Contains(string(body), expectedPrivate) || string(body) == legacy || strings.Contains(string(body), u.UUID) {
		t.Fatalf("device config: %d %s", code, body)
	}
	code, second, _ := r.c.do("GET", otherPath+"?client=wireguard", nil, nil)
	if code != 200 || string(body) == string(second) {
		t.Fatal("devices share credentials")
	}
	// A global template and entry extra must not widen this credential's protocol scope.
	if err := r.st.SetSetting(ctx, store.SettingSubTemplates, store.SubTemplates{"clash": "proxies: [{name: injected-secret, type: socks5, server: secret.example.com, port: 1080}]\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.DB().Exec(`UPDATE entries SET client_extra=? WHERE id=?`, `{"type":"socks5","password":"injected-secret"}`, e.ID); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"wireguard", "clash", "stash", "singbox", "egern"} {
		code, b, _ := r.c.do("GET", path+"?client="+format, nil, nil)
		if code != 200 || !strings.Contains(string(b), expectedPrivate) || strings.Contains(string(b), "ordinary.example.com") || strings.Contains(string(b), "injected-secret") || strings.Contains(string(b), u.UUID) {
			t.Fatalf("scope %s: %d %s", format, code, b)
		}
	}
	for _, format := range []string{"uri", "surge", "page"} {
		if code, _, _ := r.c.do("GET", path+"?client="+format, nil, nil); code != 400 {
			t.Fatal("unsupported format returned empty success", format, code)
		}
	}
	if code, _, _ := r.c.do("GET", path+"?client=clash&entry="+itoa(ordinary.ID), nil, nil); code != 404 {
		t.Fatal("non-WG selector accepted", code)
	}
	st := nativeState(t, r)
	if devicePeerCount(st, ib.Tag, u.AgentID) != 2 || devicePeerCount(st, "in", u.AgentID) != 0 {
		t.Fatal("device peers escaped WG scope", st)
	}
	for _, p := range st.Users {
		if len(p.WireGuardPeers) > 0 {
			t.Fatal("global users leaked device peers")
		}
	}
	if code, _, _ := r.c.do("DELETE", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices/"+itoa(d.ID), nil, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("revoked token survived", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 1 {
		t.Fatal("revoked peer survived state rebuild")
	}
	if code, b, _ := r.c.do("GET", "/sub/"+u.SubToken+"?client=clash", nil, nil); code != 200 || !strings.Contains(string(b), "ordinary.example.com") {
		t.Fatal("ordinary subscription changed", code, string(b))
	}
}

func TestNativeWireGuardDeviceAuthorizationAndUnavailable(t *testing.T) {
	r, u, ib, e := nativeWGFixture(t)
	ctx := context.Background()
	_, path := createNativeDevice(t, r, u.ID, "Phone")
	if _, err := r.st.DB().Exec(`UPDATE nodes SET last_seen_at=? WHERE id=?`, time.Now().Add(-10*time.Minute).Unix(), r.nodeID); err != nil {
		t.Fatal(err)
	}
	code, b, _ := r.c.do("GET", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices", nil, nil)
	view := mustJSON[service.WireGuardDevicesView](t, b)
	if code != 200 || view.Available || view.Reason != "unavailable" {
		t.Fatal(code, view)
	}
	if code, _, _ := r.c.do("POST", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices", map[string]string{"name": "New"}, nil); code != 409 {
		t.Fatal("stale registration allowed", code)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 200 {
		t.Fatal("brief offline removed existing subscription", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 1 {
		t.Fatal("offline removed peer")
	}
	nativeWGCapability(t, r, "v0.77.0", false)
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 503 {
		t.Fatal("unsupported core not retriable", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 0 {
		t.Fatal("unsupported core received device")
	}
	nativeWGCapability(t, r, "v0.76.1", true)
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 0 {
		t.Fatal("old agent received device")
	}
	nativeWGCapability(t, r, "v0.77.0", true)
	if err := r.st.SetUserEntryBlocks(ctx, u.ID, []int64{e.ID}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("blocked entry published", code)
	}
	if err := r.st.SetUserEntryBlocks(ctx, u.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.DB().Exec(`UPDATE inbounds SET enabled=0 WHERE id=?`, ib.ID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("disabled inbound published", code)
	}
	if _, err := r.st.DB().Exec(`UPDATE inbounds SET enabled=1 WHERE id=?`, ib.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.DB().Exec(`UPDATE subscriptions SET expires_at=1 WHERE user_id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("expired account published", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 0 {
		t.Fatal("expired user in state")
	}
	if _, err := r.st.DB().Exec(`UPDATE users SET status='banned' WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 403 {
		t.Fatal("banned user accepted", code)
	}
}

func TestNativeWireGuardDevicesRBACOwnershipAndRotation(t *testing.T) {
	r, u, ib, _ := nativeWGFixture(t)
	ctx := context.Background()
	d, path := createNativeDevice(t, r, u.ID, "Phone")
	base := "/api/admin/users/" + itoa(u.ID) + "/wireguard-devices"
	anon := &client{t: t, srv: r.srv}
	if code, _, _ := anon.do("GET", base, nil, nil); code != 401 {
		t.Fatal(code)
	}
	for _, role := range []string{"support", "operator"} {
		staff, err := admin.NewUser(role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.st.CreateStaff(ctx, staff); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, nil)
		code, b, h := c.do("GET", base, nil, nil)
		if code != 200 || h.Get("Cache-Control") != "no-store" {
			t.Fatal(role, code)
		}
		view := mustJSON[service.WireGuardDevicesView](t, b)
		if len(view.Devices) != 1 || (view.Devices[0].SubscriptionURL != "") != (role == "operator") {
			t.Fatal("RBAC credential leak", role, view)
		}
		if role == "support" {
			if code, _, _ := c.do("DELETE", base+"/"+itoa(d.ID), nil, nil); code != 403 {
				t.Fatal("support revoked device", code)
			}
		}
	}
	otherID, _ := r.user("other-wg@example.com")
	other, _ := r.st.UserByID(ctx, otherID)
	portal := &client{t: t, srv: r.srv}
	code, b, _ := portal.do("POST", "/api/portal/login", map[string]string{"email": other.Email, "password": "password123"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		if code, _, _ := portal.do(method, "/api/portal/me/wireguard-devices/"+itoa(d.ID), map[string]string{"name": "Stolen"}, nil); code != 404 {
			t.Fatal("cross-user device mutation", method, code)
		}
	}
	// Warm the agent cache before a portal mutation; the new peer must invalidate it.
	if code, _, _ := r.agent.do("GET", "/api/agent/state", nil, nil); code != 200 {
		t.Fatal(code)
	}
	code, b, _ = portal.do("POST", "/api/portal/me/wireguard-devices", map[string]string{"name": "Other phone"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, stateBody, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
	state := mustJSON[agentproto.State](t, stateBody)
	if code != 200 || devicePeerCount(&state, ib.Tag, other.AgentID) != 1 {
		t.Fatal("portal create did not invalidate cached agent state")
	}
	job := bulkPreview(t, r, store.UserBulkRequest{IDs: []int64{u.ID}, Action: "rotate_subscription"})
	bulkExecute(t, r, job)
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("bulk rotation retained device token", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 0 {
		t.Fatal("bulk rotation retained peer")
	}
	if _, err := r.st.WireGuardDeviceByToken(ctx, strings.TrimPrefix(path, "/sub/")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestNativeWireGuardDeviceRequiredHWIDSharedQuota(t *testing.T) {
	r, u, _, _ := nativeWGFixture(t)
	ctx := context.Background()
	if err := r.st.SetSetting(ctx, service.SettingSubscription, service.SubscriptionSettings{HWID: service.HWIDSettings{Enabled: true, Require: true, FallbackLimit: 2}}); err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := r.st.ClaimHwidDevice(ctx, u.ID, store.HwidDevice{Hwid: "ordinary-device"}, 2, time.Now()); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	d, path := createNativeDevice(t, r, u.ID, "Native")
	if code, b, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 200 || !strings.Contains(string(b), "[Interface]") {
		t.Fatal("native config requires unsupported HWID header", code, string(b))
	}
	code, b, _ := r.c.do("GET", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices", nil, nil)
	v := mustJSON[service.WireGuardDevicesView](t, b)
	if code != 200 || v.Limit != 2 || v.Remaining != 0 || v.SharedUsed == nil || *v.SharedUsed != 2 {
		t.Fatal("shared remaining wrong", code, v)
	}
	if code, _, _ := r.c.do("POST", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices", map[string]string{"name": "Overflow"}, nil); code != 409 {
		t.Fatal("shared quota bypass", code)
	}
	if allowed, count, err := r.st.ClaimHwidDevice(ctx, u.ID, store.HwidDevice{Hwid: "another-device"}, 2, time.Now()); err != nil || allowed || count != 2 {
		t.Fatal(allowed, count, err)
	}
	if code, _, _ := r.c.do("DELETE", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices/"+itoa(d.ID), nil, nil); code != 200 {
		t.Fatal(code)
	}
	if allowed, count, err := r.st.ClaimHwidDevice(ctx, u.ID, store.HwidDevice{Hwid: "another-device"}, 2, time.Now()); err != nil || !allowed || count != 2 {
		t.Fatal(allowed, count, err)
	}
}

func TestNativeWireGuardDeviceGroupAndClientChainScope(t *testing.T) {
	r, u, ib, e := nativeWGFixture(t)
	ctx := context.Background()
	_, path := createNativeDevice(t, r, u.ID, "Phone")
	group, err := r.st.CreateGroup(ctx, "Restricted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.st.DB().Exec(`UPDATE inbounds SET group_id=? WHERE id=?`, group, ib.ID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=wireguard", nil, nil); code != 404 {
		t.Fatal("unauthorized group published", code)
	}
	if devicePeerCount(nativeState(t, r), ib.Tag, u.AgentID) != 0 {
		t.Fatal("unauthorized group received peers")
	}
	if _, err = r.st.DB().Exec(`UPDATE inbounds SET group_id=NULL WHERE id=?`, ib.ID); err != nil {
		t.Fatal(err)
	}
	ibs, err := r.st.InboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	front := domain.Entry{Name: "Non-WG front", InboundID: ibs[0].ID, DisplayHost: "front.example.com", DisplayPort: 443, Enabled: true}
	if err = r.st.CreateEntry(ctx, &front); err != nil {
		t.Fatal(err)
	}
	if _, err = r.st.DB().Exec(`UPDATE entries SET client_dialer_entry_id=? WHERE id=?`, front.ID, e.ID); err != nil {
		t.Fatal(err)
	}
	code, b, _ := r.c.do("GET", "/api/admin/users/"+itoa(u.ID)+"/wireguard-devices", nil, nil)
	v := mustJSON[service.WireGuardDevicesView](t, b)
	if code != 200 || v.Available || v.Reason != "client_chain_unsupported" {
		t.Fatal(code, v)
	}
	if code, b, _ := r.c.do("GET", path+"?client=clash", nil, nil); code != 404 || strings.Contains(string(b), "front.example.com") {
		t.Fatal("device credential exposed chained front", code, string(b))
	}
}

func TestNativeWireGuardDeviceMixedUnavailableDoesNotClearEntries(t *testing.T) {
	r, u, ib, known := nativeWGFixture(t)
	ctx := context.Background()
	_, path := createNativeDevice(t, r, u.ID, "Phone")
	unconfirmed := &domain.Node{Name: "Pending WG"}
	if err := r.st.CreateNode(ctx, unconfirmed, "pending-wg", time.Hour); err != nil {
		t.Fatal(err)
	}
	second := ib
	second.ID = 0
	second.NodeID = unconfirmed.ID
	if err := r.st.CreateInbound(ctx, &second); err != nil {
		t.Fatal(err)
	}
	e := domain.Entry{Name: "Pending WG", InboundID: second.ID, DisplayHost: "pending.example.com", DisplayPort: 51820, Enabled: true}
	if err := r.st.CreateEntry(ctx, &e); err != nil {
		t.Fatal(err)
	}
	if code, b, _ := r.c.do("GET", path+"?client=clash", nil, nil); code != 503 {
		t.Fatal("partial capability failure silently removed a device entry", code, string(b))
	}
	if code, b, _ := r.c.do("GET", path+"?client=wireguard&entry="+itoa(known.ID), nil, nil); code != 200 || !strings.Contains(string(b), "[Interface]") {
		t.Fatal("unrelated capability blocked explicit working entry", code, string(b))
	}
}
