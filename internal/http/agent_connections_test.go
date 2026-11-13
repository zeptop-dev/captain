package http

import (
	"context"
	"errors"
	"github.com/zeptop-dev/captain/internal/agenthttps"
	"net/http"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestAgentConnectionsPermissionsAndMetadata(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	anonymous := &client{t: t, srv: r.srv}
	for _, scope := range []string{"nodes:write", "agent-connections:read", "agent-connections:write"} {
		tok, _, err := r.st.CreateAPIToken(ctx, 1, "migration scope", "full", nil, []string{scope})
		if err != nil {
			t.Fatal(err)
		}
		code, _, _ := anonymous.do("GET", "/api/admin/agent-connections", nil, map[string]string{"Authorization": "Bearer " + tok})
		want := 403
		if scope == "agent-connections:read" {
			want = 200
		}
		if code != want {
			t.Fatal(scope, code, want)
		}
	}
	operator, _ := admin.NewUser("operator@example.com", "password123", "operator")
	if err := r.st.CreateStaff(ctx, operator); err != nil {
		t.Fatal(err)
	}
	op := &client{t: t, srv: r.srv}
	op.do("POST", "/api/admin/login", map[string]string{"Email": operator.Email, "Password": "password123"}, nil)
	if code, _, _ := op.do("POST", "/api/admin/agent-connections/migrate", map[string]any{}, nil); code != 403 {
		t.Fatal("operator migrated agents", code)
	}
	hash := auth.SHA256Hex(r.agent.token)
	if err := r.st.ObserveAgentConnection(ctx, r.nodeID, hash, "https://old.example.com", true); err != nil {
		t.Fatal(err)
	}
	code, body, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
	if code != 200 {
		t.Fatal(code)
	}
	before := mustJSON[agentproto.State](t, body)
	req := map[string]any{"node_ids": []int64{r.nodeID}, "mode": "custom", "url": "https://agent.example.com:2096"}
	code, body, _ = r.c.do("POST", "/api/admin/agent-connections/preview", req, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	preview := mustJSON[map[string]any](t, body)
	req["revision"] = preview["revision"]
	code, body, _ = r.c.do("POST", "/api/admin/agent-connections/migrate", req, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	code, body, _ = r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConnectionVersionHeader: agentproto.ConnectionVersion, "If-None-Match": `"` + before.Revision + `"`})
	if code != 200 {
		t.Fatal("command did not wake unchanged state", code)
	}
	after := mustJSON[agentproto.State](t, body)
	if after.ConnectionMigration == nil {
		t.Fatal("missing command")
	}
	if before.ConfigRevision != after.ConfigRevision || before.Revision != after.Revision {
		t.Fatal("migration changed proxy configuration identity")
	}
	code, body, _ = r.c.do("GET", "/api/admin/agent-connections", nil, nil)
	if code != 200 || strings.Contains(string(body), after.ConnectionMigration.ProofKey) {
		t.Fatal("migration secret exposed")
	}
	code, _, _ = r.agent.do("GET", "/api/agent/connection-proof?id="+after.ConnectionMigration.ID+"&challenge="+strings.Repeat("a", 64), nil, nil)
	if code != http.StatusBadRequest {
		t.Fatal("authenticated proof request accepted", code)
	}
	code, _, _ = anonymous.do("GET", "/api/agent/connection-proof?id="+after.ConnectionMigration.ID+"&challenge="+strings.Repeat("a", 64), nil, nil)
	if code != http.StatusPreconditionFailed {
		t.Fatal("unbound proof was accepted without end-to-end TLS", code)
	}
	// The old single-entry API must not provide a back door around retirement checks.
	settings, err := r.st.AgentHTTPSSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Config.Enabled = false
	settings.Config.URL = "https://old.example.com"
	settings.Config.Listen = ":2096"
	if _, err = r.st.SaveAgentHTTPS(ctx, settings); err != nil {
		t.Fatal(err)
	}
	entries, err := r.st.AgentEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var legacy store.AgentEndpoint
	for _, e := range entries {
		if e.Default {
			legacy = e
		}
	}
	if err = r.st.DeleteAgentEndpoint(ctx, legacy.ID, legacy.Revision); err == nil {
		t.Fatal("retired referenced source")
	}
}

func TestLegacyAgentEntryWriteCannotReplaceNonDefaultReference(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	chain, key := certimateChain(t)
	cert, err := store.ParseCertificate("example.com", chain, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.UpsertCertificate(ctx, cert); err != nil {
		t.Fatal(err)
	}
	old, err := r.st.AgentHTTPSSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old.Config = agenthttps.Config{Enabled: true, URL: "https://example.com:2096", Listen: ":2096", CertificateID: cert.ID}
	if _, err = r.st.SaveAgentHTTPS(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = r.st.ObserveAgentConnection(ctx, r.nodeID, auth.SHA256Hex(r.agent.token), old.URL, true); err != nil {
		t.Fatal(err)
	}
	other, err := r.st.SaveAgentEndpoint(ctx, store.AgentEndpoint{Name: "Other", Default: true, Config: agenthttps.Config{Enabled: true, URL: "https://agent.example.com:2096", Listen: ":2096", CertificateID: cert.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.DeleteAgentEndpoint(ctx, other.ID, other.Revision); err != nil {
		t.Fatal(err)
	}
	disabled, err := r.st.AgentHTTPSSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	disabled.URL = "https://different.example.com:2096"
	if _, err = r.st.SaveAgentHTTPS(ctx, disabled); !errors.Is(err, store.ErrAgentHTTPSInvalid) {
		t.Fatal("legacy write replaced a still-used non-default entry", err)
	}
	entries, err := r.st.AgentEndpoints(ctx)
	if err != nil || len(entries) != 1 || entries[0].URL != old.URL || !entries[0].Enabled {
		t.Fatal(entries, err)
	}
}
