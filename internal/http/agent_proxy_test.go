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

func TestAgentProxyProofThroughRealTLSReverseProxy(t *testing.T) {
	r := newRig(t)
	hash := auth.SHA256Hex(r.agent.token)
	ctx := context.Background()
	origin, _ := url.Parse(r.srv.URL)
	forward := httputil.NewSingleHostReverseProxy(origin)
	prior := forward.Director
	forward.Director = func(req *http.Request) { prior(req); req.Host = origin.Host }
	proxy := httptest.NewTLSServer(forward)
	defer proxy.Close()
	choice := store.AgentConnectionChoice{Mode: "custom", URL: proxy.URL, VerificationMode: "trusted-proxy", TrustProxy: true}
	r.st.ObserveAgentConnection(ctx, r.nodeID, hash, "https://old.example.com", true, true)
	rev, _, e := r.st.PreviewAgentConnections(ctx, []int64{r.nodeID}, choice, "")
	if e != nil {
		t.Fatal(e)
	}
	_, e = r.st.StartAgentConnectionBatch(ctx, []int64{r.nodeID}, choice, "", rev)
	if e != nil {
		t.Fatal(e)
	}
	// Fresh pull capability is required even if the database says capable.
	headers := map[string]string{agentproto.ConnectionVersionHeader: agentproto.ConnectionVersion}
	code, b, _ := r.agent.do("GET", "/api/agent/state", nil, headers)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	st := mustJSON[agentproto.State](t, b)
	if st.ConnectionMigration != nil {
		t.Fatal("legacy process got proxy command")
	}
	headers[agentproto.ConnectionProxyVersionHeader] = agentproto.ConnectionProxyVersion
	code, b, _ = r.agent.do("GET", "/api/agent/state", nil, headers)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	st = mustJSON[agentproto.State](t, b)
	if st.ConnectionMigration == nil {
		t.Fatal("missing new command")
	}
	m := st.ConnectionMigration
	q := strings.Repeat("a", 64)
	res, e := proxy.Client().Get(proxy.URL + "/api/agent/connection-proof?id=" + m.ID + "&challenge=" + q)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var proof agentproto.ConnectionProof
	json.NewDecoder(res.Body).Decode(&proof)
	if res.StatusCode != 200 || !agentproto.VerifyConnectionProof(m.ProofKey, m.ID, m.TargetURL, q, proof.Signature, agentproto.ProxyConnectionBinding(m.ExpiresAt)) {
		t.Fatal(res.StatusCode, proof)
	}
	req, _ := http.NewRequest("POST", proxy.URL+"/api/agent/connection-result", strings.NewReader(`{"id":"`+m.ID+`","status":"completed"}`))
	req.Header.Set("Authorization", "Bearer "+r.agent.token)
	req.Header.Set(agentproto.ConnectionVersionHeader, agentproto.ConnectionVersion)
	req.Header.Set(agentproto.ConnectionURLHeader, proxy.URL)
	result, e := proxy.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	result.Body.Close()
	if result.StatusCode != 200 {
		t.Fatal(result.StatusCode)
	}
	v, e := r.st.AgentConnection(ctx, r.nodeID)
	if e != nil || v.Status != "completed" || v.VerificationMode != "trusted-proxy" {
		t.Fatal(v, e)
	}
}
