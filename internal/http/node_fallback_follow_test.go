package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestFallbackFollowHTTPPolicyPermissionsAndLegacyPreservation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	path := "/api/admin/settings/nodes/" + itoa(r.nodeID) + "/addresses"
	get := "/api/admin/nodes/" + itoa(r.nodeID) + "/addresses"
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	yes := map[string]any{"fallback_auto_ipv4": true, "fallback_auto_ipv6": true}
	call(r.agent, "PUT", path, yes, 401)
	call(&client{t: t, srv: r.srv}, "PUT", path, yes, 401)
	staff, err := admin.NewUser("fallback-operator@example.com", "password123", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.CreateStaff(ctx, staff); err != nil {
		t.Fatal(err)
	}
	operator := &client{t: t, srv: r.srv}
	call(operator, "POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, 200)
	call(operator, "PUT", path, yes, 403)
	for _, tc := range []struct {
		role   string
		scopes []string
		want   int
	}{{"read", []string{"settings:*"}, 403}, {"full", []string{"nodes:*"}, 403}, {"full", []string{"settings:write"}, 200}} {
		token, _, err := r.st.CreateAPIToken(ctx, 1, "follow-policy", tc.role, nil, tc.scopes)
		if err != nil {
			t.Fatal(err)
		}
		call(&client{t: t, srv: r.srv, token: token}, "PUT", path, yes, tc.want)
	}
	p := mustJSON[store.NodeAddresses](t, call(r.c, "GET", get, nil, 200))
	if !p.FallbackAutoIPv4 || !p.FallbackAutoIPv6 || p.AutoIPv4 || p.AutoIPv6 {
		t.Fatal("unexpected policy", p)
	}
	at := time.Now().Unix()
	f := &agentproto.PublicAddressFallback{Address: "203.0.113.30", Origin: "https://panel.example.com:2096", Source: agentproto.PublicAddressFallbackSource, SampledAt: at, CheckStatus: agentproto.PublicAddressFallbackCheckNoConflict, RunID: strings.Repeat("a", 32)}
	call(r.agent, "POST", "/api/agent/report", agentproto.Report{PublicAddresses: &agentproto.PublicAddresses{SampledAt: at, FallbackIPv4: f}}, 200)
	p = mustJSON[store.NodeAddresses](t, call(r.c, "PUT", path, map[string]any{"auto_ipv4": true}, 200))
	if !p.AutoIPv4 || !p.FallbackAutoIPv4 || !p.FallbackAutoIPv6 || p.Observed.FallbackFollowIPv4.ConfirmedAt != 0 {
		t.Fatal("old editor lost policy or applied old sample", p)
	}
	call(r.c, "PUT", path, map[string]any{}, 200)
	// The saved proof is visible privately, but enabling never converts it to consensus.
	p = mustJSON[store.NodeAddresses](t, call(r.c, "GET", get, nil, 200))
	if p.Observed.IPv4.Address != "" || p.Observed.FallbackIPv4 == nil || p.Observed.FallbackIPv4.CheckStatus != "no_conflict" {
		t.Fatal(p)
	}
	// An old-agent observation is valid to report but not eligible to follow.
	f.SampledAt = at + 1
	f.CheckStatus = ""
	f.RunID = ""
	call(r.agent, "POST", "/api/agent/report", agentproto.Report{PublicAddresses: &agentproto.PublicAddresses{SampledAt: at + 1, FallbackIPv4: f}}, 200)
	p = mustJSON[store.NodeAddresses](t, call(r.c, "GET", get, nil, 200))
	if p.Observed.FallbackFollowIPv4.CandidateSince != 0 {
		t.Fatal("old agent authorized", p)
	}
	call(r.c, "POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/repair", nil, 200)
	p = mustJSON[store.NodeAddresses](t, call(r.c, "GET", get, nil, 200))
	if !p.FallbackAutoIPv4 || p.Observed.FallbackIPv4 != nil || p.Observed.FallbackFollowIPv4.CandidateSince != 0 {
		t.Fatal("repair retained evidence or lost policy", p)
	}
}
