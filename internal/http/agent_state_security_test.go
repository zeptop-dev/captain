package http

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func securityRequest(t *testing.T, c *client, method, path string, body any) []byte {
	t.Helper()
	code, out, _ := c.do(method, path, body, nil)
	if code != 200 {
		t.Fatalf("%s %s returned status %d", method, path, code)
	}
	return out
}

func securityNode(t *testing.T, r *rig, name string) (int64, *client) {
	t.Helper()
	node := mustJSON[map[string]any](t, securityRequest(t, r.c, "POST", "/api/admin/nodes", map[string]string{"Name": name}))
	a := &client{t: t, srv: r.srv}
	paired := securityRequest(t, a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)})
	a.token = mustJSON[agentproto.PairResponse](t, paired).Token
	return int64(node["id"].(float64)), a
}

func securityState(t *testing.T, a *client) agentproto.State {
	t.Helper()
	return mustJSON[agentproto.State](t, securityRequest(t, a, "GET", "/api/agent/state", nil))
}

func securityUsers(t *testing.T, got, want []spec.User) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("credential count: got %d, want %d", len(got), len(want))
		return
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("user %d changed order or credential, limit, or accounting fields", want[i].ID)
		}
	}
}

func TestAgentStateWithoutEnabledInboundsHasNoCredentials(t *testing.T) {
	r := newRig(t)
	r.user("unrelated@example.com")
	all := securityState(t, r.agent).Users
	if len(all) != 1 {
		t.Fatal("fixture must contain one active customer")
	}
	nodeID, a := securityNode(t, r, "empty-full")
	t.Run("new-full-without-inbounds", func(t *testing.T) {
		state := securityState(t, a)
		if len(state.Node.Inbounds) != 0 {
			t.Fatal("new full node unexpectedly has inbounds")
		}
		securityUsers(t, state.Users, nil)
	})
	ib := mustJSON[domain.Inbound](t, securityRequest(t, r.c, "POST", "/api/admin/nodes/"+itoa(nodeID)+"/inbounds", map[string]any{"Tag": "disabled", "Protocol": "vless", "Port": 443}))
	path := "/api/admin/inbounds/" + itoa(ib.ID)
	securityRequest(t, r.c, "PATCH", path, map[string]bool{"Enabled": false})
	t.Run("only-disabled-ungrouped-inbound", func(t *testing.T) {
		state := securityState(t, a)
		if len(state.Node.Inbounds) != 0 {
			t.Fatal("disabled inbound was delivered")
		}
		securityUsers(t, state.Users, nil)
	})
	securityRequest(t, r.c, "PATCH", path, map[string]bool{"Enabled": true})
	t.Run("enable-ungrouped-inbound", func(t *testing.T) {
		securityUsers(t, securityState(t, a).Users, all)
	})
	securityRequest(t, r.c, "PATCH", path, map[string]bool{"Enabled": false})
	t.Run("disable-ungrouped-inbound-again", func(t *testing.T) {
		securityUsers(t, securityState(t, a).Users, nil)
	})
}

func TestAgentStateCredentialsFollowInboundGroupUnion(t *testing.T) {
	r := newRig(t)
	group := func(name string) int64 {
		g := mustJSON[map[string]any](t, securityRequest(t, r.c, "POST", "/api/admin/groups", map[string]string{"Name": name}))
		return int64(g["id"].(float64))
	}
	groupB, groupC := group("group-b"), group("group-c")
	customer := func(email string, groupID int64) (int64, string) {
		id, uuid := r.user(email)
		securityRequest(t, r.c, "PATCH", "/api/admin/users/"+itoa(id), map[string]any{"Status": "active", "GroupID": groupID})
		return id, uuid
	}
	_, uuidB := customer("group-b@example.com", groupB)
	_, uuidC := customer("group-c@example.com", groupC)
	sharedID, uuidShared := customer("group-b-c@example.com", groupB)
	r.user("other@example.com")
	planC := mustJSON[domain.Plan](t, securityRequest(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "shared-group-c", "PriceCents": 1, "PeriodDays": 30, "GroupID": groupC, "SpeedLimitMbps": 7, "DeviceLimit": 2}))
	securityRequest(t, r.c, "POST", "/api/admin/users/"+itoa(sharedID)+"/grant", map[string]int64{"PlanID": planC.ID})
	all := securityState(t, r.agent).Users
	if len(all) != 4 {
		t.Fatal("fixture must contain four active customers")
	}
	selected := func(names ...string) []spec.User {
		wanted := map[string]bool{}
		for _, name := range names {
			wanted[name] = true
		}
		var out []spec.User
		for _, user := range all {
			if wanted[user.UUID] {
				out = append(out, user)
			}
		}
		return out
	}
	nodeID, a := securityNode(t, r, "group-union")
	inbound := func(tag string, port int, groupID *int64) domain.Inbound {
		return mustJSON[domain.Inbound](t, securityRequest(t, r.c, "POST", "/api/admin/nodes/"+itoa(nodeID)+"/inbounds", map[string]any{"Tag": tag, "Protocol": "vless", "Port": port, "GroupID": groupID}))
	}
	b := inbound("group-b", 443, &groupB)
	c := inbound("group-c", 8443, &groupC)
	assert := func(name string, want []spec.User) {
		t.Run(name, func(t *testing.T) { securityUsers(t, securityState(t, a).Users, want) })
	}
	assert("group-b-c-union-without-unrelated-customer", selected(uuidB, uuidC, uuidShared))
	t.Run("inbound-memberships-stay-separate", func(t *testing.T) {
		state := securityState(t, a)
		if len(state.Node.Inbounds) != 2 {
			t.Fatal("expected both grouped inbounds")
		}
		for _, ib := range state.Node.Inbounds {
			if !ib.ScopedUsers {
				t.Fatal("grouped inbound lost its explicit scope")
			}
			switch ib.Tag {
			case "group-b":
				securityUsers(t, ib.Users, selected(uuidB, uuidShared))
			case "group-c":
				securityUsers(t, ib.Users, selected(uuidC, uuidShared))
			default:
				t.Fatal("unexpected inbound")
			}
		}
	})
	open := inbound("ungrouped", 9443, nil)
	assert("enabled-ungrouped-inbound-allows-all", all)
	openPath := "/api/admin/inbounds/" + itoa(open.ID)
	securityRequest(t, r.c, "PATCH", openPath, map[string]bool{"Enabled": false})
	assert("disabled-ungrouped-inbound-restores-group-union", selected(uuidB, uuidC, uuidShared))
	securityRequest(t, r.c, "PATCH", openPath, map[string]bool{"Enabled": true})
	assert("reenabled-ungrouped-inbound-allows-all", all)
	securityRequest(t, r.c, "DELETE", openPath, nil)
	assert("removed-ungrouped-inbound-restores-group-union", selected(uuidB, uuidC, uuidShared))
	securityRequest(t, r.c, "PATCH", "/api/admin/inbounds/"+itoa(c.ID), map[string]bool{"Enabled": false})
	assert("disabled-group-c-removes-exclusive-members", selected(uuidB, uuidShared))
	securityRequest(t, r.c, "DELETE", "/api/admin/inbounds/"+itoa(b.ID), nil)
	assert("removed-last-enabled-inbound-removes-all", nil)
}

func TestAgentJobAuthorizationAndCrossNodeReport(t *testing.T) {
	r := newRig(t)
	r.user("customer@example.com")
	user := &client{t: t, srv: r.srv}
	if code, _, _ := user.do("POST", "/api/portal/login", map[string]string{"email": "customer@example.com", "password": "password123"}, nil); code != 200 {
		t.Fatalf("portal login status %d", code)
	}
	base := "/api/admin/nodes/" + itoa(r.nodeID)
	for name, c := range map[string]*client{"anonymous": {t: t, srv: r.srv}, "customer": user, "agent": r.agent} {
		for _, kind := range []string{"shell", "reality_scan", spec.NetworkDiagnosticKind, spec.CoreManagementKind} {
			code, _, _ := c.do("POST", base+"/jobs", map[string]any{"kind": kind, "params": map[string]string{"command": "echo harmless"}}, nil)
			if code != 401 && code != 403 {
				t.Fatalf("%s job %s status %d", name, kind, code)
			}
		}
		code, _, _ := c.do("POST", "/api/admin/settings/nodes/"+itoa(r.nodeID)+"/host", spec.HostRequest{Action: "inspect"}, nil)
		if code != 401 && code != 403 {
			t.Fatalf("%s host status %d", name, code)
		}
	}
	if code, _, _ := r.c.do("POST", base+"/jobs", map[string]any{"kind": "shell", "params": map[string]string{"command": "echo harmless"}}, nil); code != 400 {
		t.Fatalf("admin arbitrary shell status %d", code)
	}
	code, body, _ := r.c.do("POST", "/api/admin/nodes", map[string]any{"Name": "other-node"}, nil)
	if code != 200 {
		t.Fatalf("create other node status %d", code)
	}
	node := mustJSON[map[string]any](t, body)
	other := int64(node["id"].(float64))
	if err := r.st.CreateNodeJob(context.Background(), "other-node-job", other, "reality_scan", nil); err != nil {
		t.Fatal(err)
	}
	rep := agentproto.Report{Jobs: []agentproto.JobResult{{ID: "other-node-job", Kind: "reality_scan", Result: json.RawMessage(`{"forged":true}`)}}}
	if code, _, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
		t.Fatalf("report status %d", code)
	}
	j, err := r.st.NodeJob(context.Background(), other, "other-node-job")
	if err != nil || j.DoneAt != nil || len(j.Result) != 0 {
		t.Fatalf("cross-node result changed: %v", err)
	}
	code, body, _ = r.agent.do("GET", "/api/agent/state?node_id="+itoa(other), nil, nil)
	if code != 200 {
		t.Fatalf("state status %d", code)
	}
	state := mustJSON[agentproto.State](t, body)
	if state.Node.ID != itoa(r.nodeID) {
		t.Fatal("node identity query override succeeded")
	}
	for _, j := range state.Jobs {
		if j.ID == "other-node-job" {
			t.Fatal("cross-node job was delivered")
		}
	}
}
