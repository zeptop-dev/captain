package http

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func diagnosticsHeaders() map[string]string {
	return map[string]string{agentproto.ConfigSessionHeader: "0123456789abcdef0123456789abcdef", agentproto.DiagnosticsVersionHeader: "1", agentproto.DiagnosticsNonceHeader: "abcdef0123456789abcdef0123456789"}
}
func diagnosticsFetch(t *testing.T, r *rig, headers map[string]string) agentproto.State {
	t.Helper()
	code, b, _ := r.agent.do("GET", "/api/agent/state", nil, headers)
	if code != 200 {
		t.Fatalf("diagnostic state %d: %s", code, b)
	}
	s := mustJSON[agentproto.State](t, b)
	if s.Diagnostics == nil {
		t.Fatal("no diagnostic control")
	}
	headers[agentproto.DiagnosticsCursorHeader] = s.Diagnostics.Epoch + ":" + strconv.FormatUint(s.Diagnostics.Generation, 10)
	headers[agentproto.DiagnosticsChallengeHeader] = s.Diagnostics.Challenge
	headers["If-None-Match"] = `"` + s.Revision + `"`
	if s.ConfigDelivery != nil {
		headers[agentproto.ConfigDeliveryHeader] = s.ConfigDelivery.ID
	}
	return s
}
func diagnosticsReady(t *testing.T, r *rig) (map[string]string, agentproto.State) {
	t.Helper()
	headers := diagnosticsHeaders()
	diagnosticsFetch(t, r, headers)
	return headers, diagnosticsFetch(t, r, headers)
}

func TestNodeDiagnosticsIsolatedFromConfigurationHistoryAndAccounting(t *testing.T) {
	r := newRig(t)
	headers, initial := diagnosticsReady(t, r)
	path := "/api/admin/node-diagnostics/" + itoa(r.nodeID)
	lease := "viewer-0000000001"
	// Global monitoring is disabled in this fixture. Explicit passive demand
	// still works, and business pause is deliberately irrelevant to this path.
	counts := func() string {
		t.Helper()
		var out string
		for _, table := range []string{"node_stats", "node_ping_stats", "node_resource_stats", "node_ping_series_stats", "node_config_deliveries"} {
			var n int64
			if err := r.st.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			out += fmt.Sprintf("%s:%d;", table, n)
		}
		return out
	}
	baseline := counts()
	before, _ := r.st.NodeByID(context.Background(), r.nodeID)
	accessAuditCall(t, r.c, "POST", path+"/leases", map[string]string{"request_id": lease}, 200)
	active := diagnosticsFetch(t, r, headers)
	if active.Revision != initial.Revision || active.ConfigRevision != initial.ConfigRevision || active.ConfigDelivery.ID != initial.ConfigDelivery.ID || active.Diagnostics.RemainingMS == 0 {
		t.Fatal("lease changed configuration or did not activate")
	}
	var changesBefore int64
	if err := r.st.DB().QueryRow(`SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	accessAuditCall(t, r.c, "PUT", path+"/leases/"+lease, nil, 200)
	sample := agentproto.DiagnosticsSample{Session: active.Diagnostics.Session, Epoch: active.Diagnostics.Epoch, Sequence: 1, SampledAt: time.Now(), ElapsedMS: 0, Interfaces: []agentproto.DiagnosticsInterface{}}
	accessAuditCall(t, r.agent, "POST", "/api/agent/diagnostics", sample, 204)
	out := accessAuditCall(t, r.c, "GET", path+"?lease_id="+lease, nil, 200)
	v := mustJSON[service.DiagnosticsSnapshot](t, out)
	if v.Status != "live" || len(v.Samples) != 1 || v.Samples[0].NetUpBytesPerSecond != nil || v.FreshForMS > 3000 {
		t.Fatalf("bad snapshot %+v", v)
	}
	if bytes.Contains(out, []byte(r.agent.token)) || bytes.Contains(out, []byte("token_hash")) {
		t.Fatal("secret exposed")
	}
	var changesAfter int64
	if err := r.st.DB().QueryRow(`SELECT total_changes()`).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore {
		t.Fatal("renew/sample/view performed a database write")
	}
	if baseline != counts() {
		t.Fatal("diagnostics wrote history, usage or new delivery")
	}
	after, _ := r.st.NodeByID(context.Background(), r.nodeID)
	if !before.LastSeenAt.Equal(*after.LastSeenAt) || before.AppliedRevision != after.AppliedRevision {
		t.Fatal("diagnostics renewed ordinary node evidence")
	}
	if code, _, _ := r.agent.do("GET", "/api/agent/state", nil, headers); code != 304 {
		t.Fatal("unchanged config/control should stay 304", code)
	}
	legacy := securityState(t, r.agent)
	if legacy.Diagnostics != nil || legacy.ConfigDelivery != nil {
		t.Fatal("temporary metadata mutated shared state")
	}
	accessAuditCall(t, r.c, "DELETE", path+"/leases/"+lease, nil, 204)
	stopped := diagnosticsFetch(t, r, headers)
	if stopped.Diagnostics.RemainingMS != 0 || stopped.Revision != initial.Revision {
		t.Fatal("release failed or changed configuration")
	}
	accessAuditCall(t, r.agent, "POST", "/api/agent/diagnostics", sample, 409)
	accessAuditCall(t, r.c, "POST", path+"/leases", map[string]string{"request_id": lease}, 404)
}

func TestNodeDiagnosticsTokenUsePauseAndReset(t *testing.T) {
	r := newRig(t)
	serviceControlHTTPReport(t, r, 1, &agentproto.ServiceControlReport{Supported: true, CheckedAt: time.Now().Unix(), Mode: "normal"})
	ctx := context.Background()
	control, err := r.st.NodeServiceControl(ctx, r.nodeID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	accessAuditCall(t, r.c, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/service-control", store.ServiceControlChange{Revision: control.Revision, Manual: &store.ServiceControlManual{Mode: "pause"}}, 200)
	headers, initial := diagnosticsReady(t, r)
	path := "/api/admin/node-diagnostics/" + itoa(r.nodeID)
	lease := "viewer-tokenuse01"
	plain, token, err := r.st.CreateAPIToken(ctx, 1, "diagnostics test", "full", nil, []string{"nodes:read", "node-diagnostics:*"})
	if err != nil {
		t.Fatal(err)
	}
	owner := &client{t: t, srv: r.srv, token: plain}
	accessAuditCall(t, owner, "POST", path+"/leases", map[string]string{"request_id": lease}, 200)
	var touched int64
	if err = r.st.DB().QueryRow(`SELECT COALESCE(last_used_at,0) FROM api_tokens WHERE id=?`, token.ID).Scan(&touched); err != nil || touched == 0 {
		t.Fatal("explicit start did not record token use", err)
	}
	if _, err = r.st.DB().Exec(`UPDATE api_tokens SET last_used_at=1 WHERE id=?`, token.ID); err != nil {
		t.Fatal(err)
	}
	accessAuditCall(t, owner, "PUT", path+"/leases/"+lease, nil, 200)
	active := diagnosticsFetch(t, r, headers)
	if active.Diagnostics.RemainingMS == 0 || active.Revision != initial.Revision {
		t.Fatal("business pause prohibited passive diagnostics")
	}
	accessAuditCall(t, owner, "GET", path+"?lease_id="+lease, nil, 200)
	if err = r.st.DB().QueryRow(`SELECT last_used_at FROM api_tokens WHERE id=?`, token.ID).Scan(&touched); err != nil || touched != 1 {
		t.Fatal("poll/renew/owner checks touched token use", err)
	}
	accessAuditCall(t, r.c, "POST", "/api/admin/system/reset", map[string]any{"password": "password123", "confirmation": "RESET", "nodes_acknowledged": true}, 200)
	accessAuditCall(t, r.agent, "POST", "/api/agent/diagnostics", agentproto.DiagnosticsSample{Session: active.Diagnostics.Session, Epoch: active.Diagnostics.Epoch, Sequence: 1, SampledAt: time.Now()}, 401)
	accessAuditCall(t, r.c, "POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, 200)
	newID, newAgent := securityNode(t, r, "new installation")
	if newID != r.nodeID {
		t.Fatal("reset fixture did not reuse node ID")
	}
	r.agent = newAgent
	diagnosticsReady(t, r)
	v := mustJSON[service.DiagnosticsSnapshot](t, accessAuditCall(t, r.c, "GET", path+"?lease_id="+lease, nil, 200))
	if v.Status != "expired" || len(v.Samples) != 0 {
		t.Fatal("reset inherited lease or samples")
	}
}

func TestNodeDiagnosticsPermissionsOwnerAndRevocation(t *testing.T) {
	r := newRig(t)
	headers, _ := diagnosticsReady(t, r)
	path := "/api/admin/node-diagnostics/" + itoa(r.nodeID)
	lease := "viewer-permission1"
	body := map[string]string{"request_id": lease}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		accessAuditCall(t, c, "POST", path+"/leases", body, 403)
		accessAuditCall(t, accessAuditToken(t, r, u.ID, "full", []string{"nodes:*", "node-diagnostics:*"}), "GET", path+"?lease_id="+lease, nil, 403)
	}
	for _, scopes := range [][]string{{"nodes:*"}, {"node-diagnostics:*"}} {
		accessAuditCall(t, accessAuditToken(t, r, 1, "full", scopes), "POST", path+"/leases", body, 403)
	}
	accessAuditCall(t, accessAuditToken(t, r, 1, "read", []string{"nodes:*", "node-diagnostics:*"}), "POST", path+"/leases", body, 403)
	owner := accessAuditToken(t, r, 1, "full", []string{"nodes:read", "node-diagnostics:*"})
	other := accessAuditToken(t, r, 1, "full", []string{"nodes:read", "node-diagnostics:*"})
	accessAuditCall(t, owner, "POST", path+"/leases", body, 200)
	active := diagnosticsFetch(t, r, headers)
	accessAuditCall(t, other, "GET", path+"?lease_id="+lease, nil, 404)
	accessAuditCall(t, other, "PUT", path+"/leases/"+lease, nil, 404)
	accessAuditCall(t, other, "DELETE", path+"/leases/"+lease, nil, 204)
	accessAuditCall(t, owner, "GET", path+"?lease_id="+lease, nil, 200)
	if _, err := r.st.DB().Exec(`DELETE FROM api_tokens`); err != nil {
		t.Fatal(err)
	}
	stopped := diagnosticsFetch(t, r, headers)
	if stopped.Diagnostics.RemainingMS != 0 {
		t.Fatal("revoked owner renewed sampling")
	}
	accessAuditCall(t, r.agent, "POST", "/api/agent/diagnostics", agentproto.DiagnosticsSample{Session: active.Diagnostics.Session, Epoch: active.Diagnostics.Epoch, Sequence: 1, SampledAt: time.Now()}, 409)
	accessAuditCall(t, owner, "GET", path+"?lease_id="+lease, nil, 403)
}

func TestNodeDiagnosticsLongPollWakesAndSafeCursor(t *testing.T) {
	r := newRig(t)
	headers, _ := diagnosticsReady(t, r)
	path := "/api/admin/node-diagnostics/" + itoa(r.nodeID)
	lease := "viewer-longpoll01"
	req, err := http.NewRequest("GET", r.srv.URL+"/api/agent/state?wait=30s", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.agent.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	done := make(chan *http.Response, 1)
	failed := make(chan error, 1)
	go func() {
		resp, err := r.srv.Client().Do(req)
		if err != nil {
			failed <- err
		} else {
			done <- resp
		}
	}()
	accessAuditCall(t, r.c, "POST", path+"/leases", map[string]string{"request_id": lease}, 200)
	select {
	case resp := <-done:
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("lease wake waited for normal long-poll deadline")
	}
	accessAuditCall(t, r.c, "GET", path+"?lease_id="+lease+"&after=9007199254740992", nil, 400)
	accessAuditCall(t, r.c, "DELETE", path+"/leases/viewer-late-00001", nil, 204)
	accessAuditCall(t, r.c, "POST", path+"/leases", map[string]string{"request_id": "viewer-late-00001"}, 404)
}
