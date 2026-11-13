package http

import (
	"context"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
	"testing"
)

func TestMonitorInsightsPermissionsAndPolicy(t *testing.T) {
	r := newRig(t)
	node := itoa(r.nodeID)
	reads := []string{"/api/admin/settings/monitor-storage", "/api/admin/settings/nodes/" + node + "/network-alerts", "/api/admin/settings/infrastructure/analytics?month=2026-11"}
	for _, path := range reads {
		if code, b, _ := r.c.do("GET", path, nil, nil); code != 200 {
			t.Fatalf("%s %d %s", path, code, b)
		}
	}
	code, body, _ := r.c.do("GET", "/api/admin/settings/infrastructure/analytics?month=2026-11", nil, nil)
	empty := mustJSON[struct {
		Prices   []store.InfraPrice    `json:"prices"`
		Expenses []store.InfraExpense  `json:"expenses"`
		Totals   []store.InfraForecast `json:"totals"`
	}](t, body)
	if code != 200 || empty.Prices == nil || empty.Expenses == nil || empty.Totals == nil {
		t.Fatal("empty collections must stay arrays", code, string(body))
	}
	if code, _, _ := r.c.do("PUT", "/api/admin/settings/nodes/"+node+"/route-watch", map[string]any{"confirmations": 2, "notify": false}, nil); code != 400 {
		t.Fatal("null expected policy accepted", code)
	}
	narrow := accessAuditToken(t, r, 1, "full", []string{"nodes:*", "monitoring:*", "infrastructure:*", "settings:*"})
	for _, path := range reads {
		if code, _, _ := narrow.do("GET", path, nil, nil); code != 403 {
			t.Fatalf("narrow token gained insights %s: %d", path, code)
		}
	}
	p := store.DefaultNetworkAlertPolicy()
	p.Mode = "adaptive"
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/nodes/"+node+"/network-alerts", p, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	p.WindowMinutes = 0
	if code, _, _ := r.c.do("PUT", "/api/admin/settings/nodes/"+node+"/network-alerts", p, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _, _ := r.c.do("PUT", "/api/admin/settings/nodes/"+node+"/route-watch", map[string]any{"expected": map[string]string{"CT": "bad"}, "confirmations": 2}, nil); code != 400 {
		t.Fatal(code)
	}
	if code, _, _ := r.c.do("POST", "/api/admin/settings/monitor-storage/maintain", nil, nil); code != 200 {
		t.Fatal(code)
	}
	for _, path := range []string{"/api/admin/settings/infrastructure/analytics?month=bad", "/api/admin/settings/infrastructure/analytics?asset_id=-1"} {
		if code, _, _ := r.c.do("GET", path, nil, nil); code != 400 {
			t.Fatal(path, code)
		}
	}
	for _, role := range []string{"operator", "support"} {
		u, e := admin.NewUser(role+"@example.com", "password123", role)
		if e != nil {
			t.Fatal(e)
		}
		if e = r.st.CreateStaff(context.Background(), u); e != nil {
			t.Fatal(e)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
		for _, path := range reads {
			if code, _, _ := c.do("GET", path, nil, nil); code != 403 {
				t.Fatal(role, path, code)
			}
		}
	}
	explicit := accessAuditToken(t, r, 1, "read", []string{"monitor-storage:read", "network-alerts:read", "infra-analytics:read"})
	for _, path := range reads {
		if code, b, _ := explicit.do("GET", path, nil, nil); code != 200 {
			t.Fatal(path, code, string(b))
		}
	}
	if code, _, _ := explicit.do("POST", "/api/admin/settings/monitor-storage/maintain", nil, nil); code != 403 {
		t.Fatal("read token wrote", code)
	}

}
