package http

import (
	"context"
	"encoding/json"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestAgentDetectionAcrossReverseProxyAndFinalConfirmation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	hash := auth.SHA256Hex(r.agent.token)
	source := "https://old.example.com"
	endpoint, _ := url.Parse(r.srv.URL)
	forward := httputil.NewSingleHostReverseProxy(endpoint)
	prior := forward.Director
	forward.Director = func(req *http.Request) { prior(req); req.Host = endpoint.Host }
	proxy := httptest.NewTLSServer(forward)
	defer proxy.Close()
	r.st.ObserveAgentConnection(ctx, r.nodeID, hash, source, true, true, true)
	req := map[string]any{"node_ids": []int64{r.nodeID}, "mode": "custom", "url": proxy.URL, "verification_mode": "auto"}
	code, b, _ := r.c.do("POST", "/api/admin/agent-connections/detect", req, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	batch := mustJSON[map[string]string](t, b)["batch_id"]
	code, b, _ = r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConnectionVersionHeader: agentproto.ConnectionVersion})
	if code != 200 {
		t.Fatal(code)
	}
	before := mustJSON[agentproto.State](t, b)
	if before.ConnectionProbe != nil {
		t.Fatal("old process received probe")
	}
	headers := map[string]string{agentproto.ConnectionProbeVersionHeader: agentproto.ConnectionProbeVersion, agentproto.ConnectionVersionHeader: agentproto.ConnectionVersion, "If-None-Match": `"` + before.Revision + `"`}
	code, b, h := r.agent.do("GET", "/api/agent/state", nil, headers)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	after := mustJSON[agentproto.State](t, b)
	p := after.ConnectionProbe
	if p == nil {
		t.Fatal("new process missing diagnostic")
	}
	if after.Revision != before.Revision || after.ConfigRevision != before.ConfigRevision {
		t.Fatal("diagnostic changed config identity")
	}
	headers[agentproto.ConnectionProbeCursorHeader] = p.ID
	if code, b, _ = r.agent.do("GET", "/api/agent/state", nil, headers); code != 304 {
		t.Fatal("delivered probe cursor did not preserve conditional polling", code, string(b))
	}
	if !strings.Contains(h.Get("Vary"), agentproto.ConnectionProbeCursorHeader) {
		t.Fatal("missing Vary")
	}
	challenge := strings.Repeat("c", 64)
	res, e := proxy.Client().Get(proxy.URL + "/api/agent/connection-proof?id=" + p.ID + "&challenge=" + challenge)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var proof agentproto.ConnectionProof
	json.NewDecoder(res.Body).Decode(&proof)
	if res.StatusCode != 200 || proof.Signature != "" || !agentproto.VerifyConnectionProof(p.ProofKey, p.ID, p.TargetURL, challenge, proof.ProxySignature, agentproto.ProbeConnectionBinding(p.ExpiresAt)) {
		t.Fatal(res.StatusCode, proof)
	}
	code, b, _ = r.agent.do("POST", "/api/agent/connection-probe-result", agentproto.ConnectionProbeResult{ID: p.ID, Status: "trusted-proxy"}, headers)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, b, _ = r.c.do("GET", "/api/admin/agent-connections/detect/"+batch, nil, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	view := mustJSON[store.AgentProbeView](t, b)
	if !view.Ready || !view.TrustProxy || view.Revision == "" {
		t.Fatal(view)
	}
	var count int
	r.st.DB().QueryRow("SELECT count(*) FROM agent_connection_migrations").Scan(&count)
	if count != 0 {
		t.Fatal("diagnostic migrated automatically")
	}
	req["detection_id"] = batch
	req["revision"] = view.Revision
	if code, _, _ = r.c.do("POST", "/api/admin/agent-connections/migrate", req, nil); code == 200 {
		t.Fatal("proxy trust not explicitly confirmed")
	}
	req["trust_proxy"] = true
	if code, b, _ = r.c.do("POST", "/api/admin/agent-connections/migrate", req, nil); code != 200 {
		t.Fatal(code, string(b))
	}
}
