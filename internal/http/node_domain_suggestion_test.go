package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeDomainSuggestionHTTPAccessAndLocks(t *testing.T) {
	testDomainSuggestionHTTPAccessAndLocks(t, "/api/admin/nodes/domain-suggestion")
}
func TestIngressDomainSuggestionHTTPAccessAndLocks(t *testing.T) {
	testDomainSuggestionHTTPAccessAndLocks(t, "/api/admin/ingresses/domain-suggestion")
}
func testDomainSuggestionHTTPAccessAndLocks(t *testing.T, path string) {
	var st *store.Store
	var blocked, writes atomic.Int32
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		if !st.Accounts.TryLock() {
			blocked.Add(1)
			w.WriteHeader(500)
			return
		}
		st.Accounts.Unlock()
		var result any = []any{}
		if r.URL.Path == "/zones" {
			result = []map[string]string{{"id": "zone"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer cf.Close()
	rig := newRigAt(t, "http://test", Options{DNSBase: cf.URL})
	st = rig.st
	ctx := context.Background()
	if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "fixture-token"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"zone": "example.com"}
	for _, scopes := range [][]string{{"nodes:*"}, {"domains:read"}, {"POST " + path}} {
		tok, _, err := st.CreateAPIToken(ctx, 1, "suggestion", "read", nil, scopes)
		if err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: rig.srv, token: tok}
		code, b, _ := c.do("POST", path, body, nil)
		want := 200
		if scopes[0] == "nodes:*" {
			want = 403
		}
		if code != want {
			t.Fatal(scopes, code, string(b))
		}
		if code == 200 && mustJSON[map[string]any](t, b)["dns_check"] != "checked" {
			t.Fatal(string(b))
		}
	}
	if code, b, _ := rig.c.do("POST", path, body, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	if blocked.Load() != 0 {
		t.Fatal("outer HTTP handler held Accounts across provider read")
	}
	if writes.Load() != 0 {
		t.Fatal("HTTP suggestion wrote provider records")
	}
	if code, _, _ := (&client{t: t, srv: rig.srv}).do("POST", path, body, nil); code != 401 {
		t.Fatal("anonymous access", code)
	}
	if code, _, _ := rig.c.do("POST", path, body, map[string]string{"Origin": "https://other.example.com"}); code != 403 {
		t.Fatal("cross origin", code)
	}
}
