package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestLineImpactPreviewAPI(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, h := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, b)
		}
		if code == 200 && strings.Contains(path, "/preview") && h.Get("Cache-Control") != "no-store" {
			t.Fatal("preview may be cached")
		}
		return b
	}
	landing, err := r.st.NodeByID(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	landing.PublicAddr = "203.0.113.30"
	if err = r.st.UpdateNode(ctx, landing); err != nil {
		t.Fatal(err)
	}
	relay := &domain.Node{Name: "Relay", PublicAddr: "192.0.2.10"}
	if err = r.st.CreateNode(ctx, relay, "relay-pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	input := store.ForwardLine{Name: "Previewed line", Protocol: "tcp", Backend: "relay", TargetInboundID: ibs[0].ID, Groups: []store.LineGroup{{Members: []store.LineMember{{NodeID: relay.ID, Port: 18443}}}}}
	logs, err := r.st.AdminLog(ctx, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	p := mustJSON[store.LinePreview](t, call(r.c, "POST", "/api/admin/lines/preview", input, 200))
	lines, err := r.st.ListLines(ctx)
	if err != nil || len(lines) != 0 {
		t.Fatal("preview created a line", err)
	}
	after, err := r.st.AdminLog(ctx, 0, 500)
	if err != nil || len(logs) != len(after) {
		t.Fatal("preview wrote audit record", err)
	}
	for _, role := range []string{"operator", "support"} {
		u, err := admin.NewUser(role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		call(c, "POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, 200)
		call(c, "POST", "/api/admin/lines/preview", input, 403)
	}
	anonymous := &client{t: t, srv: r.srv}
	call(anonymous, "POST", "/api/admin/lines/preview", input, 401)
	code, _, _ := r.c.do("POST", "/api/admin/lines/preview", input, map[string]string{"Origin": "https://other.example.com"})
	if code != 403 {
		t.Fatal("cross-site preview accepted", code)
	}
	for _, grant := range []string{"entries:read", "nodes:write"} {
		tok, _, err := r.st.CreateAPIToken(ctx, 1, "restricted", "full", nil, []string{grant})
		if err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv, token: tok}
		call(c, "POST", "/api/admin/lines/preview", input, 403)
	}
	checked := p.Draft
	checked.PreviewRevision = p.Revision
	saved := mustJSON[store.ForwardLine](t, call(r.c, "POST", "/api/admin/lines", checked, 200))
	path := "/api/admin/lines/" + itoa(saved.ID)
	call(r.c, "GET", path+"/dependencies", nil, 200)
	call(anonymous, "GET", path+"/dependencies", nil, 401)
	p = mustJSON[store.LinePreview](t, call(r.c, "POST", path+"/preview", saved, 200))
	checked = p.Draft
	checked.PreviewRevision = p.Revision
	if _, err = r.st.DB().Exec(`UPDATE nodes SET public_addr='198.51.100.20' WHERE id=?`, r.nodeID); err != nil {
		t.Fatal(err)
	}
	call(r.c, "PUT", path, checked, 409)
	actual, err := r.st.LineByID(ctx, saved.ID)
	if err != nil || actual.Version != saved.Version {
		t.Fatal("stale save changed line", err)
	}
	// Old API callers retain the existing immediate-save contract.
	actual.Name = "Legacy save"
	call(r.c, "PUT", path, actual, 200)
}
