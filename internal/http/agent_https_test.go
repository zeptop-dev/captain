package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

const agentHTTPSPath = "/api/admin/settings/agent-https"

type agentHTTPSView struct {
	Settings store.AgentHTTPSSettings `json:"settings"`
	Status   agenthttps.Status        `json:"status"`
	Ready    bool                     `json:"ready"`
}

func waitAgentHTTPS(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Agent HTTPS state did not converge")
}

func TestAgentHTTPSSettingsLifecycleAndIsolation(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = r.web.AgentHTTPS().Close(context.Background()) })
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := r.c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	get := func() agentHTTPSView { return mustJSON[agentHTTPSView](t, call("GET", agentHTTPSPath, nil, 200)) }
	initial := get()
	if initial.Settings.Enabled || initial.Ready || initial.Settings.Revision == "" {
		t.Fatalf("unexpected default: %+v", initial)
	}
	chain, key := certimateChain(t)
	c, err := store.ParseCertificate("example.com", chain, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.UpsertCertificate(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Keep the occupied port reserved until the failed binding has been observed.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	_, port, _ := net.SplitHostPort(occupied.Addr().String())
	in := initial.Settings
	in.Enabled, in.URL, in.Listen, in.CertificateID = true, "https://example.com:"+port, occupied.Addr().String(), c.ID
	wrong := in
	wrong.URL = "https://other.example.net:" + port
	call("PUT", agentHTTPSPath, wrong, 400)
	reserved := in
	reserved.URL, reserved.Listen = "https://example.com:8080", ":8080"
	call("PUT", agentHTTPSPath, reserved, 400)
	call("PUT", agentHTTPSPath, in, 200)
	go r.web.AgentHTTPS().Run(ctx)
	waitAgentHTTPS(t, func() bool { return r.web.AgentHTTPS().Status().State == "error" })
	if get().Ready {
		t.Fatal("occupied port reported ready")
	}
	call("GET", "/api/health", nil, 200)
	_ = occupied.Close()
	in = get().Settings
	active := mustJSON[agentHTTPSView](t, call("PUT", agentHTTPSPath, in, 200))
	if !active.Ready || active.Status.ActiveURL != in.URL {
		t.Fatalf("entry not active: %+v", active)
	}
	// Old drafts cannot overwrite the selected entry, and certificate deletion
	// is checked in the same database transaction as the selection.
	call("PUT", agentHTTPSPath, initial.Settings, 409)
	call("DELETE", "/api/admin/certificates/"+itoa(c.ID), nil, 409)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(chain))
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, in.Listen)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	httpClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(method, path, token string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, in.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	for _, path := range []string{"/admin/", "/portal/", "/api/admin/me", "/api/health", "/api/hooks/certificate", "/sub/example", "/"} {
		resp := request("GET", path, r.agent.token)
		if resp.StatusCode != 404 {
			t.Fatalf("non-agent route %s exposed: %d", path, resp.StatusCode)
		}
	}
	resp := request("GET", "/api/agent/health", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte(`"service":"captain-agent"`)) || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("health: %d %s", resp.StatusCode, body)
	}
	challenge := strings.Repeat("a", 64)
	resp = request("GET", "/api/agent/health?challenge="+challenge, "")
	body, _ = io.ReadAll(resp.Body)
	proof := mustJSON[map[string]any](t, body)["proof"]
	if proof != r.web.agentCheck.Proof(challenge) {
		t.Fatal("dedicated listener did not authenticate this process")
	}
	if code, _, _ := r.c.do("GET", "/api/agent/health", nil, nil); code != 404 {
		t.Fatal("Agent-specific health appeared on the main listener")
	}
	resp = request("GET", "/api/agent/state", r.agent.token)
	if resp.StatusCode != 200 {
		t.Fatalf("existing token rejected on dedicated listener: %d", resp.StatusCode)
	}
	resp.Body.Close()
	if resp = request("GET", "/api/agent/state", "wrong"); resp.StatusCode != 401 {
		t.Fatalf("bad token accepted: %d", resp.StatusCode)
	}
	if resp = request("GET", "/api/agent/downloads/invalid", ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated download: %d", resp.StatusCode)
	}
	// A failed switch selects a new certificate in the DB but keeps the old
	// listener. Both certificates remain protected until that intent is fixed.
	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocked.Close() })
	_, blockedPort, _ := net.SplitHostPort(blocked.Addr().String())
	other, err := store.ParseCertificate("*.example.com", chain, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.UpsertCertificate(ctx, other); err != nil {
		t.Fatal(err)
	}
	next := get().Settings
	next.URL, next.Listen, next.CertificateID = "https://agent.example.com:"+blockedPort, blocked.Addr().String(), other.ID
	failed := mustJSON[agentHTTPSView](t, call("PUT", agentHTTPSPath, next, 200))
	if failed.Ready || failed.Status.State != "error" || failed.Status.CertificateID != c.ID {
		t.Fatalf("failed switch: %+v", failed)
	}
	call("DELETE", "/api/admin/certificates/"+itoa(c.ID), nil, 409)
	call("DELETE", "/api/admin/certificates/"+itoa(other.ID), nil, 409)
	in.Revision = failed.Settings.Revision
	call("PUT", agentHTTPSPath, in, 200)
	call("DELETE", "/api/admin/certificates/"+itoa(other.ID), nil, 200)
	// A renewal updates the existing row. The controller must hot-load it;
	// the website and configured installation origin stay unchanged.
	newChain, newKey := certimateChain(t)
	newCert, err := store.ParseCertificate("example.com", newChain, newKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.UpsertCertificate(ctx, newCert); err != nil {
		t.Fatal(err)
	}
	pool.AppendCertsFromPEM([]byte(newChain))
	transport.CloseIdleConnections()
	newPair, err := tls.X509KeyPair([]byte(newChain), []byte(newKey))
	if err != nil {
		t.Fatal(err)
	}
	waitAgentHTTPS(t, func() bool {
		transport.CloseIdleConnections()
		resp := request("GET", "/api/agent/health", "")
		resp.Body.Close()
		return bytes.Equal(resp.TLS.PeerCertificates[0].Raw, newPair.Certificate[0])
	})
	call("GET", "/api/health", nil, 200)
	disabled := get().Settings
	disabled.Enabled = false
	result := mustJSON[agentHTTPSView](t, call("PUT", agentHTTPSPath, disabled, 200))
	if result.Ready || result.Status.State != "disabled" {
		t.Fatalf("disable: %+v", result)
	}
	disabled = result.Settings
	disabled.CertificateID = 0
	call("PUT", agentHTTPSPath, disabled, 200)
	call("DELETE", "/api/admin/certificates/"+itoa(c.ID), nil, 200)
}

func TestAgentHTTPSResetRejectsInitialDraft(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	before, err := r.st.AgentHTTPSSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.st.Accounts.Lock()
	_, err = r.st.ResetSite(ctx, 1)
	r.web.AgentHTTPS().Refresh(ctx)
	r.st.Accounts.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	after, err := r.st.AgentHTTPSSettings(ctx)
	if err != nil || after.Enabled || after.CertificateID != 0 || after.URL != "" || after.Revision == before.Revision {
		t.Fatalf("reset entry %+v / %v", after, err)
	}
	before.URL = "https://stale.example.com:2096"
	if _, err = r.st.SaveAgentHTTPS(ctx, before); !errors.Is(err, store.ErrAgentHTTPSRevision) {
		t.Fatalf("stale pre-reset draft accepted: %v", err)
	}
}

func TestAgentHTTPSPermissionsAndCAS(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	anonymous := &client{t: t, srv: r.srv}
	if code, _, _ := anonymous.do("GET", agentHTTPSPath, nil, nil); code != 401 {
		t.Fatal(code)
	}
	for _, role := range []string{"operator", "support"} {
		u, _ := admin.NewUser(role+"@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
		if code, _, _ := c.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": "0"}, nil); code != 403 {
			t.Fatal("role accessed diagnostics", role, code)
		}
		for _, method := range []string{"GET", "PUT"} {
			if code, _, _ := c.do(method, agentHTTPSPath, map[string]any{}, nil); code != 403 {
				t.Fatalf("%s %s: %d", role, method, code)
			}
		}
	}
	for _, scope := range []string{"settings:read", "settings:write", "agent-https:read", "agent-https:write", "nodes:read"} {
		token, _, err := r.st.CreateAPIToken(ctx, 1, "entry test", "full", nil, []string{scope})
		if err != nil {
			t.Fatal(err)
		}
		h := map[string]string{"Authorization": "Bearer " + token}
		want := 403
		if scope == "agent-https:read" {
			want = 200
		}
		if code, _, _ := anonymous.do("GET", agentHTTPSPath, nil, h); code != want {
			t.Fatalf("%s read %d want %d", scope, code, want)
		}
		if code, b, _ := anonymous.do("GET", "/api/admin/nodes/agent-https", nil, h); scope == "nodes:read" {
			if code != 200 || strings.Contains(string(b), "certificate") || strings.Contains(string(b), "listen") {
				t.Fatalf("node summary: %d %s", code, b)
			}
		} else if code != 403 {
			t.Fatalf("summary escaped scope %s: %d", scope, code)
		}
		want = 403
		if scope == "agent-https:write" {
			want = 409
		}
		if code, _, _ := anonymous.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": "stale"}, h); code != want {
			t.Fatalf("%s diagnostic permission: %d want %d", scope, code, want)
		}
		want = 403
		if scope == "agent-https:write" {
			want = 409
		}
		if code, _, _ := anonymous.do("PUT", agentHTTPSPath, map[string]any{"revision": "stale"}, h); code != want {
			t.Fatalf("%s write %d want %d", scope, code, want)
		}
	}
	code, raw, _ := r.c.do("GET", agentHTTPSPath, nil, nil)
	if code != 200 {
		t.Fatal(code)
	}
	v := mustJSON[agentHTTPSView](t, raw).Settings
	if code, _, _ := r.c.do("PUT", agentHTTPSPath, v, map[string]string{"Origin": "https://other.example.com"}); code != 403 {
		t.Fatal("cross-origin write accepted", code)
	}
	// Both writers read the same revision: exactly one different update wins.
	results := make(chan error, 2)
	for _, url := range []string{"https://one.example.com:2096", "https://two.example.com:2096"} {
		in := v
		in.URL = url
		go func() { _, err := r.st.SaveAgentHTTPS(ctx, in); results <- err }()
	}
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, store.ErrAgentHTTPSRevision)) || (b == nil && errors.Is(a, store.ErrAgentHTTPSRevision))) {
		t.Fatalf("CAS: %v / %v", a, b)
	}
}
