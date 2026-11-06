package http

import (
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestDNSHealthPermissionsHistoryPaginationAndReset(t *testing.T) {
	r := newMigratedRig(t)
	ctx := context.Background()
	nodePath := "/api/admin/nodes/" + itoa(r.nodeID) + "/dns-health"
	paths := []string{nodePath, "/api/admin/monitoring/dns", "/api/admin/settings/dns/history"}
	anon := &client{t: t, srv: r.srv}
	for _, path := range paths {
		if code, _, _ := anon.do("GET", path, nil, nil); code != 401 {
			t.Fatalf("anonymous %s %d", path, code)
		}
	}
	for _, role := range []string{"operator", "support"} {
		u, _ := admin.NewUser(role+"@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
		for _, path := range paths {
			want := 200
			if role == "support" || path == paths[2] {
				want = 403
			}
			if code, b, _ := c.do("GET", path, nil, nil); code != want {
				t.Fatalf("%s %s %d %s", role, path, code, b)
			}
		}
		want := 200
		if role == "support" {
			want = 403
		}
		if code, b, _ := c.do("POST", nodePath+"/check", nil, nil); code != want {
			t.Fatalf("check %s %d %s", role, code, b)
		}
	}
	// The fixture has no domain: this checks routing without public DNS I/O.
	if code, b, _ := r.c.do("POST", nodePath+"/check", nil, nil); code != 200 {
		t.Fatalf("check %d %s", code, b)
	}
	for range 53 {
		c := store.DNSChange{NodeID: &r.nodeID, Source: "node", Actor: "admin@example.com", Zone: "example.com", Name: "node.example.com", Action: "updated"}
		if err := r.st.BeginDNSChange(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	_, b, _ := r.c.do("GET", paths[2]+"?node_id="+itoa(r.nodeID), nil, nil)
	page := mustJSON[struct {
		Items []store.DNSChange `json:"items"`
		More  bool              `json:"more"`
	}](t, b)
	if len(page.Items) != 50 || !page.More {
		t.Fatalf("unbounded history %s", b)
	}
	_, b, _ = r.c.do("GET", paths[2]+"?before="+itoa(page.Items[49].ID), nil, nil)
	next := mustJSON[struct {
		Items []store.DNSChange `json:"items"`
		More  bool              `json:"more"`
	}](t, b)
	if len(next.Items) != 3 || next.More {
		t.Fatalf("pagination %s", b)
	}
	if code, _, _ := r.c.do("GET", paths[2]+"?before=-1", nil, nil); code != 400 {
		t.Fatal("invalid cursor")
	}
	if code, _, _ := r.c.do("GET", "/api/admin/nodes/9999/dns-health", nil, nil); code != 404 {
		t.Fatal("missing node")
	}
	if code, b, _ := r.c.do("POST", resetPath, resetInput(), nil); code != 200 {
		t.Fatalf("reset %d %s", code, b)
	}
	for _, table := range []string{"node_dns_health", "dns_changes"} {
		var count int
		if err := r.st.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("reset left %s %d %v", table, count, err)
		}
	}
}
