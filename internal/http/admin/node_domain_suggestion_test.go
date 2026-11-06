package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/testdb"
)

type suggestionSession struct {
	st *store.Store
	id int64
}

func (s suggestionSession) Resolve(ctx context.Context, _ string) (*domain.User, bool, error) {
	u, e := s.st.StaffByID(ctx, s.id)
	return u, true, e
}
func (suggestionSession) Create(context.Context, int64, bool) (*domain.Session, error) {
	panic("not used")
}
func (suggestionSession) Delete(context.Context, string) error { panic("not used") }
func suggestionRig(t *testing.T, provider string, token string) (*handlers, *store.Domain, *domain.User) {
	t.Helper()
	st := store.New(testdb.Open(t))
	u := &domain.User{Email: "admin@example.com", Role: domain.RoleAdmin, Status: "active"}
	if e := st.CreateStaff(context.Background(), u); e != nil {
		t.Fatal(e)
	}
	z := &store.Domain{Name: "example.com", Provider: provider, CFToken: token, AutoDNS: true}
	if e := st.CreateDomain(context.Background(), z); e != nil {
		t.Fatal(e)
	}
	h := &handlers{Deps: Deps{Store: st, BaseURL: "https://panel.example.com", Sessions: suggestionSession{st, u.ID}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DNS: &service.DNS{Store: st}}}
	return h, z, u
}
func requestSuggestion(h *handlers, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "https://panel.example.com/api/admin/nodes/domain-suggestion", strings.NewReader(body))
	r.Pattern = "POST /api/admin/nodes/domain-suggestion"
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "fixture"})
	w := httptest.NewRecorder()
	h.suggestNodeDomain(w, r)
	return w
}
func TestNodeDomainSuggestionDNSReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, provider, token, status, reason string
		occupied, fail                        bool
	}{
		{name: "manual", provider: "manual", status: "unverified", reason: "manual_provider"},
		{name: "no-token", provider: "cloudflare", status: "unverified", reason: "missing_token"},
		{name: "cloudflare", provider: "cloudflare", token: "fixture-token", status: "checked"},
		{name: "skip-existing-cname", provider: "cloudflare", token: "fixture-token", status: "checked", occupied: true},
		{name: "failed-provider", provider: "cloudflare", token: "fixture-token", status: "unverified", reason: "provider_unavailable", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, z, _ := suggestionRig(t, tc.provider, tc.token)
			// Disabling auto updates does not disable explicit read-only DNS checking.
			z.AutoDNS = false
			if e := h.Store.UpdateDomain(context.Background(), z); e != nil {
				t.Fatal(e)
			}
			var requests, writes atomic.Int32
			first := ""
			cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(500)
					return
				}
				if tc.fail {
					w.WriteHeader(500)
					_, _ = w.Write([]byte(`{"success":false}`))
					return
				}
				var result any = []any{}
				if r.URL.Path == "/zones" {
					result = []map[string]string{{"id": "zone"}}
				} else {
					if r.URL.Query().Has("type") || r.URL.Query().Get("name") == "" {
						t.Error("must check all record types at exact name")
					}
					if first == "" {
						first = r.URL.Query().Get("name")
						if tc.occupied {
							result = []map[string]string{{"type": "CNAME", "name": first, "content": "other.example.com"}}
						}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
			}))
			defer cf.Close()
			h.DNS.Base = cf.URL
			w := requestSuggestion(h, `{"zone":"EXAMPLE.COM."}`)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var v nodeDomainSuggestion
			if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
				t.Fatal(e)
			}
			if v.DNSCheck != tc.status || v.DNSReason != tc.reason {
				t.Fatalf("wrong evidence %+v", v)
			}
			if !regexp.MustCompile(`^[a-z]+-[23456789abcdefghjkmnpqrstuvwxyz]{4}$`).MatchString(v.Prefix) || v.Domain != v.Prefix+".example.com" {
				t.Fatalf("invalid suggestion %+v", v)
			}
			if tc.occupied && v.Domain == first {
				t.Fatal("returned occupied CNAME")
			}
			if writes.Load() != 0 {
				t.Fatal("random button wrote DNS")
			}
			if tc.token == "" && requests.Load() != 0 {
				t.Fatal("unexpected DNS request")
			}
			nodes, e := h.Store.ListNodes(context.Background())
			if e != nil || len(nodes) != 0 {
				t.Fatal("random button saved a node", e)
			}
			log, e := h.Store.AdminLog(context.Background(), 0, 100)
			if e != nil || len(log) != 0 {
				t.Fatal("read-only suggestion logged as mutation", e)
			}
		})
	}
}
func TestNodeDomainSuggestionRevalidatesAfterNetwork(t *testing.T) {
	for _, mode := range []string{"revoke", "zone", "claim", "delegate"} {
		t.Run(mode, func(t *testing.T) {
			h, z, u := suggestionRig(t, "cloudflare", "fixture-token")
			var lockFailure atomic.Bool
			cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !h.Store.Accounts.TryLock() {
					lockFailure.Store(true)
					w.WriteHeader(500)
					return
				}
				defer h.Store.Accounts.Unlock()
				var result any = []any{}
				if r.URL.Path == "/zones" {
					result = []map[string]string{{"id": "zone"}}
				} else {
					switch mode {
					case "revoke":
						if e := h.Store.UpdateStaff(r.Context(), u.ID, "disabled", ""); e != nil {
							t.Error(e)
						}
					case "zone":
						z.CFToken = "changed-token"
						if e := h.Store.UpdateDomain(r.Context(), z); e != nil {
							t.Error(e)
						}
					case "claim":
						n := &domain.Node{Name: "new claim", Domain: r.URL.Query().Get("name")}
						if e := h.Store.CreateNode(r.Context(), n, "fixture", time.Hour); e != nil {
							t.Error(e)
						}
					case "delegate":
						if e := h.Store.CreateDomain(r.Context(), &store.Domain{Name: r.URL.Query().Get("name"), Provider: "manual"}); e != nil {
							t.Error(e)
						}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
			}))
			defer cf.Close()
			h.DNS.Base = cf.URL
			w := requestSuggestion(h, `{"zone":"example.com"}`)
			want := 409
			if mode == "revoke" {
				want = 403
			}
			if lockFailure.Load() {
				t.Fatal("provider request held account lock")
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"prefix"`) {
				t.Fatal("stale suggestion escaped")
			}
		})
	}
}
func TestNodeDomainSuggestionRejectsBadInputsAndRoles(t *testing.T) {
	h, _, _ := suggestionRig(t, "manual", "")
	for _, body := range []string{`{}`, `{"zone":"*.example.com"}`, `{"zone":"https://example.com"}`, `{"zone":"example.com","url":"https://example.com"}`, `{"zone":"example.com"} {}`, strings.Repeat("x", 5000)} {
		if w := requestSuggestion(h, body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := requestSuggestion(h, `{"zone":"unknown.example.com"}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		if allowed(role, http.MethodPost, "/api/admin/nodes/domain-suggestion") {
			t.Fatal("role permitted", role)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "https://panel.example.com/api/admin/nodes/domain-suggestion", strings.NewReader(`{"zone":"example.com"}`))
	w := httptest.NewRecorder()
	h.suggestNodeDomain(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "fixture"})
	r.Header.Set("Origin", "https://evil.example.com")
	w = httptest.NewRecorder()
	h.suggestNodeDomain(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestNodeDomainSuggestionBoundsOccupiedRetries(t *testing.T) {
	h, _, _ := suggestionRig(t, "cloudflare", "fixture-token")
	var records atomic.Int32
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any = []map[string]string{{"id": "zone"}}
		if r.URL.Path != "/zones" {
			records.Add(1)
			result = []map[string]string{{"type": "TXT", "name": r.URL.Query().Get("name")}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer cf.Close()
	h.DNS.Base = cf.URL
	w := requestSuggestion(h, `{"zone":"example.com"}`)
	if w.Code != 409 || records.Load() != 12 {
		t.Fatal(w.Code, records.Load(), w.Body.String())
	}
}
func TestNodeDomainSuggestionDoesNotLockSlowBody(t *testing.T) {
	h, _, _ := suggestionRig(t, "manual", "")
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	r := httptest.NewRequest(http.MethodPost, "https://panel.example.com/api/admin/nodes/domain-suggestion", reader)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "fixture"})
	done := make(chan struct{})
	go func() { defer close(done); h.suggestNodeDomain(httptest.NewRecorder(), r) }()
	// Wait until the handler consumes part of the body before testing the lock.
	if _, e := writer.Write([]byte(`{"zone":`)); e != nil {
		t.Fatal(e)
	}
	if !h.Store.Accounts.TryLock() {
		t.Fatal("slow request body holds Accounts")
	}
	h.Store.Accounts.Unlock()
	_, _ = writer.Write([]byte(`"example.com"}`))
	_ = writer.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish")
	}
}

func TestNodeDomainSuggestionClaims(t *testing.T) {
	h, _, _ := suggestionRig(t, "manual", "")
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	n := &domain.Node{Name: "fixture", Domain: "node.example.com"}
	must(h.Store.CreateNode(ctx, n, "fixture", time.Hour))
	ib := &domain.Inbound{NodeID: n.ID, Tag: "tls", Protocol: spec.Trojan, Port: 443, Enabled: true, Settings: spec.Inbound{TLS: &spec.TLS{Mode: spec.TLSStandard, ServerName: "tls.example.com"}}}
	must(h.Store.CreateInbound(ctx, ib))
	must(h.Store.CreateEntry(ctx, &domain.Entry{Name: "fixture entry", InboundID: ib.ID, DisplayHost: "entry.example.com", DisplayPort: 443, Enabled: true}))
	must(h.Store.CreateIngress(ctx, &store.Ingress{Name: "fixture ingress", NodeID: n.ID, EntryDomain: "ingress.example.com", EntryHost: "198.51.100.20", PortFrom: 10000, PortTo: 11000}))
	must(h.Store.UpsertCertificate(ctx, &store.Certificate{Domain: "cert.example.com", Names: []string{"cert.example.com", "san.example.com", "*.example.com"}, NotAfter: time.Now().Add(time.Hour)}))
	must(h.Store.SetSetting(ctx, service.SettingSubscription, service.SubscriptionSettings{URLs: []string{"https://sub.example.com:8443/path"}}))
	must(h.Store.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Hosts: []string{"probe.example.com"}}))
	h.AgentURL = "https://agent.example.com:2096"
	names, err := h.nodeDomainClaims(ctx)
	must(err)
	for _, name := range []string{"example.com", "node.example.com", "tls.example.com", "entry.example.com", "ingress.example.com", "cert.example.com", "san.example.com", "sub.example.com", "probe.example.com", "panel.example.com", "agent.example.com"} {
		if !names[name] {
			t.Error("missing panel claim", name)
		}
	}
	if names["*.example.com"] || names["198.51.100.20"] {
		t.Fatal("wildcard certificate or IP treated as an exact hostname claim")
	}
}

func TestNodeDomainSuggestionUnicodeRegisteredZone(t *testing.T) {
	h, _, _ := suggestionRig(t, "manual", "")
	ctx := context.Background()
	z := &store.Domain{Name: "bücher.example", Provider: "cloudflare", CFToken: "unicode-fixture-token"}
	if err := h.Store.CreateDomain(ctx, z); err != nil {
		t.Fatal(err)
	}
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any = []any{}
		if r.URL.Path == "/zones" {
			if r.URL.Query().Get("name") != "xn--bcher-kva.example" {
				t.Error("provider zone was not canonical ASCII", r.URL)
			}
			result = []map[string]string{{"id": "zone"}}
		} else if !strings.HasSuffix(r.URL.Query().Get("name"), ".xn--bcher-kva.example") {
			t.Error("provider hostname was not canonical ASCII", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer unicode-fixture-token" {
			t.Error("wrong zone token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer cf.Close()
	h.DNS.Base = cf.URL
	for _, name := range []string{"bücher.example", "xn--bcher-kva.example"} {
		body, _ := json.Marshal(map[string]string{"zone": name})
		w := requestSuggestion(h, string(body))
		if w.Code != 200 {
			t.Fatalf("registered %s: %d %s", name, w.Code, w.Body.String())
		}
		var result nodeDomainSuggestion
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.DNSCheck != "checked" || !strings.HasSuffix(result.Domain, ".xn--bcher-kva.example") {
			t.Fatal(w.Body.String())
		}
	}
}

func TestNodeDomainSuggestionEquivalentCatalogNames(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "during-provider-check"}[late], func(t *testing.T) {
			h, _, _ := suggestionRig(t, "manual", "")
			ctx := context.Background()
			if err := h.Store.CreateDomain(ctx, &store.Domain{Name: "bücher.example", Provider: "cloudflare", CFToken: "first-token"}); err != nil {
				t.Fatal(err)
			}
			duplicate := func() {
				t.Helper()
				if err := h.Store.CreateDomain(ctx, &store.Domain{Name: "xn--bcher-kva.example", Provider: "cloudflare", CFToken: "different-token"}); err != nil {
					t.Fatal(err)
				}
			}
			if !late {
				duplicate()
			}
			var calls atomic.Int32
			cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var result any = []any{}
				if r.URL.Path == "/zones" {
					result = []map[string]string{{"id": "zone"}}
				} else {
					duplicate()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
			}))
			defer cf.Close()
			h.DNS.Base = cf.URL
			w := requestSuggestion(h, `{"zone":"bücher.example"}`)
			if w.Code != 409 || strings.Contains(w.Body.String(), `"prefix"`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if !late && calls.Load() != 0 {
				t.Fatal("ambiguous catalog chose a provider token")
			}
		})
	}
}

func TestNodeDomainSuggestionLongestCanonicalSuffix(t *testing.T) {
	h, _, _ := suggestionRig(t, "manual", "")
	ctx := context.Background()
	parent := &store.Domain{Name: "bücher.example", Provider: "manual"}
	child := &store.Domain{Name: "edge.bücher.example", Provider: "cloudflare", CFToken: "child-token"}
	for _, z := range []*store.Domain{parent, child} {
		if err := h.Store.CreateDomain(ctx, z); err != nil {
			t.Fatal(err)
		}
	}
	match, err := h.suggestionDomainFor(ctx, "host.edge.xn--bcher-kva.example")
	if err != nil || match == nil || match.ID != child.ID || match.Name != child.Name || match.CFToken != child.CFToken {
		t.Fatal("longest normalized match did not retain original row", match, err)
	}
	saved, err := h.suggestionZone(ctx, "edge.xn--bcher-kva.example")
	if err != nil || saved.Domain.ID != child.ID || saved.Token != child.CFToken {
		t.Fatal(saved, err)
	}
}
