package http

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
)

func TestAgentMigrationDeliveredCursorKeepsConditionalState(t *testing.T) {
	for _, mode := range []string{"direct", "trusted-proxy"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			if err := r.st.ObserveAgentConnection(context.Background(), r.nodeID, auth.SHA256Hex(r.agent.token), "https://old.example.com", true, true); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"node_ids": []int64{r.nodeID}, "mode": "custom", "url": "https://agent.example.com:2096", "verification_mode": mode, "trust_proxy": mode == "trusted-proxy"}
			body := accessAuditCall(t, r.c, "POST", "/api/admin/agent-connections/preview", input, 200)
			preview := mustJSON[map[string]any](t, body)
			input["revision"] = preview["revision"]
			accessAuditCall(t, r.c, "POST", "/api/admin/agent-connections/migrate", input, 200)
			headers := map[string]string{agentproto.ConnectionVersionHeader: agentproto.ConnectionVersion, agentproto.ConnectionProxyVersionHeader: agentproto.ConnectionProxyVersion, agentproto.StandaloneAuthHeader: agentproto.StandaloneAuthVersion}
			code, body, reply := r.agent.do("GET", "/api/agent/state", nil, headers)
			if code != 200 {
				t.Fatal(code, string(body))
			}
			state := mustJSON[agentproto.State](t, body)
			if state.ConnectionMigration == nil {
				t.Fatal("missing initial migration command")
			}
			vary := reply.Get("Vary")
			for _, name := range []string{agentproto.ConnectionProxyVersionHeader, agentproto.StandaloneAuthHeader} {
				if !strings.Contains(vary, name) {
					t.Fatal("capability omitted from Vary", name)
				}
			}
			headers["If-None-Match"] = `"` + state.Revision + `"`
			headers[agentproto.ConnectionCursorHeader] = state.ConnectionMigration.ID
			code, _, _ = r.agent.do("GET", "/api/agent/state", nil, headers)
			if code != http.StatusNotModified {
				t.Fatalf("already delivered %s cursor returned %d instead of304; unchanged long polls would repeatedly return bodies", mode, code)
			}
			delete(headers, agentproto.ConnectionCursorHeader)
			code, _, _ = r.agent.do("GET", "/api/agent/state", nil, headers)
			if code != 200 {
				t.Fatal("undelivered migration did not wake unchanged state", code)
			}
		})
	}
}
