package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/store"
)

type dnsSyncProvider struct {
	mu      sync.Mutex
	records map[string][]dns.Record
	writes  int
	reject  string
}

func (p *dnsSyncProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var result any
	switch {
	case r.URL.Path == "/zones":
		result = []map[string]string{{"id": "zone"}}
	case r.Method == http.MethodGet:
		result = p.records[r.URL.Query().Get("type")]
		if result == nil {
			result = []dns.Record{}
		}
	default:
		p.writes++
		if p.reject != "" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"success":false}`))
			return
		}
		var rec dns.Record
		_ = json.NewDecoder(r.Body).Decode(&rec)
		rec.ID = "record-" + rec.Type
		p.records[rec.Type] = []dns.Record{rec}
		result = rec
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}
func syncPlan(t *testing.T, d *DNS, id int64) *DNSNodeSyncPlan {
	t.Helper()
	p, err := d.PrepareNodeSync(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	d.ReadNodeSyncPlan(context.Background(), p)
	return p
}
func TestManualDNSSyncReviewsWritesAndInvalidates(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	n.V6Addr = "2001:db8::20"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "secret-test-token", AutoDNS: true}); err != nil {
		t.Fatal(err)
	}
	provider := &dnsSyncProvider{records: map[string][]dns.Record{"A": {{ID: "record-A", Type: "A", Name: n.Domain, Content: "198.51.100.20", TTL: 600}}}}
	srv := httptest.NewServer(provider)
	defer srv.Close()
	d := &DNS{Store: st, Base: srv.URL}
	p := syncPlan(t, d, n.ID)
	if !p.Enabled || len(p.Records) != 2 || p.Records[0].Action != "updated" || p.Records[1].Action != "created" || p.Records[0].Before.TTL != 600 || provider.writes != 0 {
		t.Fatalf("invalid read-only plan %+v", p)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "secret-test-token") || strings.Contains(string(b), "Identity") {
		t.Fatal("private plan leaked")
	}
	health := &DNSHealth{Store: st, Lookup: func(context.Context, string, string) ([]string, error) { return []string{n.PublicAddr, n.V6Addr}, nil }}
	if _, err := health.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	result, err := d.ApplyNodeSync(WithDNSOrigin(ctx, DNSOrigin{NodeID: n.ID, Source: "manual_sync", Actor: "admin@example.com"}), p, p.Revision)
	if err != nil || len(result) != 2 || provider.writes != 2 {
		t.Fatalf("apply %v %v", result, err)
	}
	for _, r := range result {
		if r.Outcome != "success" || r.Error != "" {
			t.Fatalf("outcome %+v", r)
		}
	}
	h, err := health.Snapshot(ctx, n.ID)
	if err != nil || !h.Stale || h.CheckedAt != 0 {
		t.Fatalf("prewrite health retained %+v %v", h, err)
	}
	history, err := st.DNSChanges(ctx, n.ID, 0)
	if err != nil || len(history) != 2 || history[0].Source != "manual_sync" {
		t.Fatalf("history %+v %v", history, err)
	}
	again := syncPlan(t, d, n.ID)
	if _, err := d.ApplyNodeSync(ctx, again, p.Revision); !errors.Is(err, ErrDNSConfigChanged) {
		t.Fatalf("old plan reused %v", err)
	}
	if _, err := d.ApplyNodeSync(ctx, again, again.Revision); err != nil || provider.writes != 2 {
		t.Fatalf("unchanged write %v", err)
	}
	// Clearing IPv6 never requests a delete or silently clears the provider's AAAA.
	n.V6Addr = ""
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	p = syncPlan(t, d, n.ID)
	if len(p.Records) != 1 {
		t.Fatal("empty v6 became a DNS action")
	}
	if _, err := d.ApplyNodeSync(ctx, p, p.Revision); err != nil {
		t.Fatal(err)
	}
	if len(provider.records["AAAA"]) != 1 || provider.writes != 2 {
		t.Fatal("empty address deleted provider record")
	}
}
func TestManualDNSSyncProtectsExternalRecordsAndChangedConfig(t *testing.T) {
	for _, kind := range []string{"shared", "missing_token", "manual", "proxy", "multiple", "conflicting_ingress", "changed_address", "changed_provider"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			st, n := dnsTestStore(t)
			dom := &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test", AutoDNS: true}
			if kind == "manual" {
				dom.Provider = "manual"
			}
			if kind == "missing_token" {
				dom.CFToken = ""
			}
			if err := st.CreateDomain(ctx, dom); err != nil {
				t.Fatal(err)
			}
			if kind == "shared" {
				n.DomainShared = true
				_ = st.UpdateNode(ctx, n)
			}
			if kind == "conflicting_ingress" {
				if err := st.CreateIngress(ctx, &store.Ingress{NodeID: n.ID, Name: "NAT", Kind: "nat", EntryDomain: n.Domain, EntryHost: "203.0.113.30"}); err != nil {
					t.Fatal(err)
				}
			}
			rec := dns.Record{ID: "A", Type: "A", Name: n.Domain, Content: "198.51.100.20", TTL: 120}
			if kind == "proxy" {
				rec.Proxied = true
			}
			provider := &dnsSyncProvider{records: map[string][]dns.Record{"A": {rec}}}
			if kind == "multiple" {
				provider.records["A"] = append(provider.records["A"], rec)
			}
			srv := httptest.NewServer(provider)
			defer srv.Close()
			d := &DNS{Store: st, Base: srv.URL}
			p := syncPlan(t, d, n.ID)
			if kind == "changed_address" {
				n.PublicAddr = "203.0.113.30"
				_ = st.UpdateNode(ctx, n)
				if _, err := d.ApplyNodeSync(ctx, p, p.Revision); !errors.Is(err, ErrDNSConfigChanged) {
					t.Fatal(err)
				}
			} else if kind == "changed_provider" {
				provider.mu.Lock()
				provider.records["A"][0].TTL = 300
				provider.mu.Unlock()
				q := syncPlan(t, d, n.ID)
				if _, err := d.ApplyNodeSync(ctx, q, p.Revision); !errors.Is(err, ErrDNSConfigChanged) {
					t.Fatal(err)
				}
			} else {
				if p.Enabled || p.Reason == "" {
					t.Fatalf("unsafe plan %+v", p)
				}
				if _, err := d.ApplyNodeSync(ctx, p, p.Revision); !errors.Is(err, ErrDNSSyncDisabled) {
					t.Fatal(err)
				}
			}
			if provider.writes != 0 {
				t.Fatal("unsafe write occurred")
			}
		})
	}
}

func TestManualDNSSyncPartialProviderOutcomeIsNotSuccess(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx := context.Background()
			st, n := dnsTestStore(t)
			n.V6Addr = "2001:db8::20"
			if err := st.UpdateNode(ctx, n); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test", AutoDNS: true}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var result any = []dns.Record{}
				if r.URL.Path == "/zones" {
					result = []map[string]string{{"id": "zone"}}
				}
				if r.Method != http.MethodGet {
					var rec dns.Record
					_ = json.NewDecoder(r.Body).Decode(&rec)
					if rec.Type == "AAAA" {
						w.WriteHeader(status)
						_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"private-error"}]}`))
						return
					}
					rec.ID = "record-A"
					result = rec
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
			}))
			defer srv.Close()
			d := &DNS{Store: st, Base: srv.URL}
			p := syncPlan(t, d, n.ID)
			results, err := d.ApplyNodeSync(WithDNSOrigin(ctx, DNSOrigin{NodeID: n.ID, Source: "manual_sync"}), p, p.Revision)
			if err != nil || len(results) != 2 {
				t.Fatalf("apply %v %v", results, err)
			}
			want := "failed"
			if status >= 500 {
				want = "unknown"
			}
			if results[0].Outcome != "success" || results[1].Outcome != want || results[1].Error == "" || strings.Contains(results[1].Error, "private-error") {
				t.Fatalf("partial outcome concealed %+v", results)
			}
			history, err := st.DNSChanges(ctx, n.ID, 0)
			if err != nil || len(history) != 2 || history[0].Outcome != want || history[1].Outcome != "success" {
				t.Fatalf("partial history %+v %v", history, err)
			}
		})
	}
}

func TestManualDNSSyncWithAutomaticDNSDisabled(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	n.V6Addr = "2001:db8::20"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	dom := &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test", AutoDNS: false}
	if err := st.CreateDomain(ctx, dom); err != nil {
		t.Fatal(err)
	}
	provider := &dnsSyncProvider{records: map[string][]dns.Record{}}
	srv := httptest.NewServer(provider)
	defer srv.Close()
	d := &DNS{Store: st, Base: srv.URL}
	for _, ip := range []string{n.PublicAddr, n.V6Addr} {
		result := d.Ensure(ctx, n.Domain, ip)
		if result.Action != "skipped" || result.Error != "" {
			t.Fatalf("automatic synchronization enabled: %+v", result)
		}
	}
	if provider.writes != 0 {
		t.Fatal("automatic synchronization wrote provider records")
	}
	p := syncPlan(t, d, n.ID)
	if !p.Enabled || p.Reason != "" || len(p.Records) != 2 {
		t.Fatalf("manual synchronization unavailable: %+v", p)
	}
	results, err := d.ApplyNodeSync(WithDNSOrigin(ctx, DNSOrigin{NodeID: n.ID, Source: "manual_sync"}), p, p.Revision)
	if err != nil || len(results) != 2 || provider.writes != 2 {
		t.Fatalf("manual synchronization failed: %+v %v", results, err)
	}
	for _, result := range results {
		if result.Outcome != "success" {
			t.Fatalf("manual synchronization outcome: %+v", result)
		}
	}
	saved, err := st.DomainByID(ctx, dom.ID)
	if err != nil || saved.AutoDNS {
		t.Fatalf("manual synchronization changed automatic preference: %+v %v", saved, err)
	}
	n.PublicAddr = "203.0.113.30"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	result := d.Ensure(ctx, n.Domain, n.PublicAddr)
	if result.Action != "skipped" || result.Error != "" || provider.writes != 2 || provider.records["A"][0].Content == n.PublicAddr {
		t.Fatalf("automatic synchronization enabled after manual operation: %+v", result)
	}
	// Credentials remain part of the reviewed identity even though the unrelated
	// automatic-write preference is intentionally not part of manual eligibility.
	p = syncPlan(t, d, n.ID)
	dom.CFToken = "rotated-test-token"
	if err := st.UpdateDomain(ctx, dom); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ApplyNodeSync(ctx, p, p.Revision); !errors.Is(err, ErrDNSConfigChanged) {
		t.Fatalf("changed credentials reused reviewed plan: %v", err)
	}
	if provider.writes != 2 {
		t.Fatal("stale manual plan wrote provider records")
	}
}
