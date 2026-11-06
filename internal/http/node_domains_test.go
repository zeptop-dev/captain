package http

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/testdb"
)

func TestNodeDomainOwnership(t *testing.T) {
	ctx := context.Background()
	conn := testdb.OpenAt(t, filepath.Join(t.TempDir(), "c.db"))
	st := store.New(conn)
	staff, _ := admin.NewUser("admin@example.com", "password123", "admin")
	if err := st.CreateStaff(ctx, staff); err != nil {
		t.Fatal(err)
	}
	cf := &fakeCloudflare{}
	cfSrv := httptest.NewServer(cf)
	defer cfSrv.Close()
	cfg := config.Default()
	cfg.BaseURL = "https://panel.example.com"
	srv := httptest.NewServer(New(cfg, st, slog.Default(), Options{DNSBase: cfSrv.URL}).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	request := func(method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := ac.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, b)
		}
		return b
	}
	request("POST", "/api/admin/login", map[string]string{"Email": "admin@example.com", "Password": "password123"}, 200)
	request("POST", "/api/admin/domains", map[string]any{"Name": "example.com", "CFToken": "test-token"}, 200)
	create := func(name, host, ip string, shared bool, want int) int64 {
		t.Helper()
		b := request("POST", "/api/admin/nodes", map[string]any{"Name": name, "Domain": host, "PublicAddr": ip, "DomainShared": shared}, want)
		if want != 200 {
			return 0
		}
		return int64(mustJSON[map[string]any](t, b)["id"].(float64))
	}
	a := create("A", " POOL.Example.com. ", "192.0.2.10", false, 200)
	if cf.get("pool.example.com", "A") != "192.0.2.10" {
		t.Fatal("exclusive DNS missing")
	}
	create("B", "pool.example.com", "198.51.100.20", false, 409)
	// Failed save must not insert a node or touch DNS.
	nodes, _ := st.ListNodes(ctx)
	if len(nodes) != 1 || cf.get("pool.example.com", "A") != "192.0.2.10" {
		t.Fatal("conflict changed state")
	}
	b := request("GET", "/api/admin/nodes/domain-check?domain=POOL.example.com.", nil, 200)
	if !strings.Contains(string(b), `"name":"A"`) || !strings.Contains(string(b), `"valid":true`) {
		t.Fatalf("preview: %s", b)
	}
	b = request("GET", "/api/admin/nodes/domain-check?domain=pool.example.com&exclude_id="+itoa(a), nil, 200)
	if !strings.Contains(string(b), `"conflicts":[]`) {
		t.Fatalf("self conflict: %s", b)
	}
	for _, bad := range []string{"a.example.com,b.example.com", "https://example.com", "example.com:443", "*.example.com"} {
		create("bad", bad, "", false, 400)
	}

	other := create("B", "pool.example.com", "198.51.100.20", true, 200)
	if cf.get("pool.example.com", "A") != "192.0.2.10" {
		t.Fatal("shared create overwrote DNS")
	}
	patch := map[string]any{"Name": "A edited", "Domain": "pool.example.com", "PublicAddr": "203.0.113.30"}
	b = request("PATCH", "/api/admin/nodes/"+itoa(a), patch, 200)
	if !strings.Contains(string(b), `"reason":"shared_domain"`) || cf.get("pool.example.com", "A") != "192.0.2.10" {
		t.Fatalf("other node overwrote shared DNS: %s", b)
	}
	// Omitted and null new fields preserve sharing for old clients.
	patch = map[string]any{"Name": "B edited", "Domain": "pool.example.com", "PublicAddr": "198.51.100.20"}
	for _, includeNull := range []bool{false, true} {
		if includeNull {
			patch["DomainShared"] = nil
		}
		request("PATCH", "/api/admin/nodes/"+itoa(other), patch, 200)
		n, _ := st.NodeByID(ctx, other)
		if !n.DomainShared {
			t.Fatal("old client cleared sharing")
		}
	}
	patch["DomainShared"] = false
	request("PATCH", "/api/admin/nodes/"+itoa(other), patch, 409)
	n, _ := st.NodeByID(ctx, other)
	if !n.DomainShared {
		t.Fatal("failed mode change persisted")
	}
	// Ingress DNS uses the same guard, including noncanonical spelling.
	b = request("POST", "/api/admin/nodes/"+itoa(a)+"/ingresses", map[string]any{"Name": "shared-line", "EntryHost": "203.0.113.30", "EntryDomain": "POOL.example.com.", "PortFrom": 20000, "PortTo": 20010}, 200)
	if !strings.Contains(string(b), `"reason":"shared_domain"`) || cf.get("pool.example.com", "A") != "192.0.2.10" {
		t.Fatalf("ingress overwrote shared DNS: %s", b)
	}
	// A single node with external DNS must also remain untouched.
	external := create("external", "cdn.example.com", "203.0.113.30", true, 200)
	if cf.get("cdn.example.com", "A") != "" {
		t.Fatal("external DNS created")
	}

	request("PATCH", "/api/admin/nodes/"+itoa(external), map[string]any{"Name": "external", "Domain": "cdn.example.com", "PublicAddr": "203.0.113.30", "DomainShared": false}, 200)
	if cf.get("cdn.example.com", "A") != "203.0.113.30" {
		t.Fatal("exclusive mode did not resume DNS")
	}

	// Simulate pre-upgrade duplicates without retroactively granting sharing.
	if _, err := conn.Exec(`UPDATE nodes SET domain_shared=0 WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	patch["DomainShared"] = nil
	b = request("PATCH", "/api/admin/nodes/"+itoa(other), patch, 200)
	if !strings.Contains(string(b), `"reason":"shared_domain"`) {
		t.Fatalf("legacy DNS not skipped: %s", b)
	}
	b = request("GET", "/api/admin/nodes/"+itoa(other), nil, 200)
	if !strings.Contains(string(b), `"domain_conflict":true`) || !strings.Contains(string(b), `"domain_shared":false`) {
		t.Fatalf("legacy warning missing: %s", b)
	}
	// Editing into an occupied name is rejected and rolls back other fields.
	c := create("C", "unique.example.com", "203.0.113.30", false, 200)
	request("PATCH", "/api/admin/nodes/"+itoa(c), patch, 409)
	n, _ = st.NodeByID(ctx, c)
	if n.Name != "C" || n.Domain != "unique.example.com" {
		t.Fatal("rejected edit changed node")
	}
	// Independent node domains cannot be overwritten by an ingress's address.
	b = request("POST", "/api/admin/nodes/"+itoa(a)+"/ingresses", map[string]any{"Name": "wrong-address", "EntryHost": "192.0.2.10", "EntryDomain": "unique.example.com", "PortFrom": 20100, "PortTo": 20110}, 200)
	if !strings.Contains(string(b), `"reason":"domain_address_conflict"`) || cf.get("unique.example.com", "A") != "203.0.113.30" {
		t.Fatalf("ingress stole node DNS: %s", b)
	}
	// With the other domain claim removed, explicitly returning to exclusive works.
	request("PATCH", "/api/admin/nodes/"+itoa(other), map[string]any{"Name": "B", "Domain": "other.example.com", "PublicAddr": "198.51.100.20"}, 200)
	request("PATCH", "/api/admin/nodes/"+itoa(a), map[string]any{"Name": "A", "Domain": "pool.example.com", "DomainShared": false, "PublicAddr": "192.0.2.10"}, 200)

	anon := &client{t: t, srv: srv}
	code, _, _ := anon.do("GET", "/api/admin/nodes/domain-check?domain=pool.example.com", nil, nil)
	if code != 401 {
		t.Fatalf("anonymous preview: %d", code)
	}
	support, _ := admin.NewUser("support@example.com", "password123", "support")
	if err := st.CreateStaff(ctx, support); err != nil {
		t.Fatal(err)
	}
	sc := &client{t: t, srv: srv}
	sc.do("POST", "/api/admin/login", map[string]string{"Email": "support@example.com", "Password": "password123"}, nil)
	code, _, _ = sc.do("GET", "/api/admin/nodes/domain-check?domain=pool.example.com", nil, nil)
	if code != 403 {
		t.Fatalf("support preview: %d", code)
	}
}
