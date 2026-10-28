package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestManualDNSSyncHTTPPermissions(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/dns-sync"
	plan := mustJSON[service.DNSNodeSyncPlan](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	if plan.Enabled || plan.Reason != "no_domain" {
		t.Fatalf("unexpected plan %+v", plan)
	}
	body := map[string]string{"revision": plan.Revision}
	accessAuditCall(t, r.c, "HEAD", path, nil, 200)
	anon := &client{t: t, srv: r.srv}
	for _, method := range []string{"GET", "POST"} {
		accessAuditCall(t, anon, method, path, body, 401)
	}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		for _, method := range []string{"GET", "POST"} {
			accessAuditCall(t, c, method, path, body, 403)
			accessAuditCall(t, accessAuditToken(t, r, u.ID, "full", []string{"*"}), method, path, body, 403)
		}
	}
	for _, scopes := range [][]string{{"nodes:*"}, {"settings:*"}, {"domains:*"}, {"monitoring:*"}} {
		c := accessAuditToken(t, r, 1, "full", scopes)
		for _, method := range []string{"GET", "POST"} {
			accessAuditCall(t, c, method, path, body, 403)
		}
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"node-dns-sync:*"})
	accessAuditCall(t, reader, "GET", path, nil, 200)
	accessAuditCall(t, reader, "POST", path, body, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"node-dns-sync:*"})
	accessAuditCall(t, writer, "GET", path, nil, 200)
	accessAuditCall(t, writer, "POST", path, body, 409)
	accessAuditCall(t, r.c, "POST", path, map[string]string{"host": "unreviewed.example.com", "ip": "203.0.113.30"}, 400)
	if code, _, _ := r.c.do("POST", path, body, map[string]string{"Origin": "https://other.example.com"}); code != 403 {
		t.Fatal("cross-origin DNS write allowed", code)
	}
}

func dnsSyncHTTPFixture(t *testing.T, handler http.Handler) (*store.Store, *client, *domain.Node, int64, int64) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	st, c, uid, tid := newSubDesignRig(t, Options{DNSBase: srv.URL})
	ctx := context.Background()
	n := &domain.Node{Name: "DNS", Domain: "node.example.com", PublicAddr: "192.0.2.10", V6Addr: "2001:db8::20"}
	if err := st.CreateNode(ctx, n, "test-pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "not-for-response", AutoDNS: true}); err != nil {
		t.Fatal(err)
	}
	return st, c, n, uid, tid
}
func TestManualDNSSyncHTTPReviewAndApply(t *testing.T) {
	cf := &fakeCloudflare{}
	st, c, n, _, _ := dnsSyncHTTPFixture(t, cf)
	path := "/api/admin/nodes/" + itoa(n.ID) + "/dns-sync"
	raw := accessAuditCall(t, c, "GET", path, nil, 200)
	if strings.Contains(string(raw), "not-for-response") {
		t.Fatal("token leaked")
	}
	p := mustJSON[service.DNSNodeSyncPlan](t, raw)
	if !p.Enabled || len(p.Records) != 2 {
		t.Fatalf("plan %+v", p)
	}
	result := mustJSON[struct {
		Results []service.Result `json:"results"`
	}](t, accessAuditCall(t, c, "POST", path, map[string]string{"revision": p.Revision}, 200))
	if len(result.Results) != 2 || cf.get(n.Domain, "A") != n.PublicAddr || cf.get(n.Domain, "AAAA") != n.V6Addr {
		t.Fatalf("missing records %+v", result)
	}
	rows, err := st.DNSChanges(context.Background(), n.ID, 0)
	if err != nil || len(rows) != 2 || rows[0].Source != "manual_sync" || rows[0].Actor != "admin@example.com" {
		t.Fatalf("missing audit %+v %v", rows, err)
	}
	accessAuditCall(t, c, "POST", path, map[string]string{"revision": p.Revision}, 409)
}
func TestManualDNSSyncHTTPReauthorizesWithoutLockingProviderReads(t *testing.T) {
	for _, change := range []string{"revoke", "address"} {
		t.Run(change, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var block atomic.Bool
			var writes atomic.Int32
			var once sync.Once
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if block.Load() && r.Method == http.MethodGet {
					once.Do(func() { close(entered) })
					<-release
				}
				var result any = []dns.Record{}
				if r.URL.Path == "/zones" {
					result = []map[string]string{{"id": "zone"}}
				}
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
			})
			st, c, n, uid, tid := dnsSyncHTTPFixture(t, handler)
			path := "/api/admin/nodes/" + itoa(n.ID) + "/dns-sync"
			p := mustJSON[service.DNSNodeSyncPlan](t, accessAuditCall(t, c, "GET", path, nil, 200))
			block.Store(true)
			done := make(chan int, 1)
			go func() { code, _, _ := c.do("POST", path, map[string]string{"revision": p.Revision}, nil); done <- code }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider read not entered")
			}
			if !st.Accounts.TryLock() {
				t.Fatal("provider read holds account lock")
			}
			if change == "revoke" {
				if err := st.DeleteAPIToken(context.Background(), uid, tid); err != nil {
					t.Fatal(err)
				}
			} else {
				if !st.Topology.TryLock() {
					t.Fatal("provider read holds topology lock")
				}
				n.PublicAddr = "203.0.113.30"
				if err := st.UpdateNode(context.Background(), n); err != nil {
					t.Fatal(err)
				}
				st.Topology.Unlock()
			}
			st.Accounts.Unlock()
			close(release)
			want := 403
			if change == "address" {
				want = 409
			}
			if code := <-done; code != want {
				t.Fatalf("stale authorization/config accepted %d", code)
			}
			if writes.Load() != 0 {
				t.Fatal("provider writes after revocation/config change")
			}
			rows, err := st.DNSChanges(context.Background(), n.ID, 0)
			if err != nil || len(rows) != 0 {
				t.Fatalf("unexpected journal %+v %v", rows, err)
			}
		})
	}
}
