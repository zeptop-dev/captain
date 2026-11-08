package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// Exercise the real HTTP middleware, ingress defaults, subscription persistence,
// and background DNS retry together. Only the external DNS provider is replaced.
func TestIngressManagedDNSAndSubscriptionHTTP(t *testing.T) {
	var mu sync.Mutex
	records := map[string]dns.Record{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var result any
		switch {
		case r.URL.Path == "/zones":
			result = []map[string]string{{"id": "zone"}}
		case r.Method == http.MethodGet:
			list := []dns.Record{}
			for _, record := range records {
				if record.Name == r.URL.Query().Get("name") && (r.URL.Query().Get("type") == "" || record.Type == r.URL.Query().Get("type")) {
					list = append(list, record)
				}
			}
			result = list
		default:
			var record dns.Record
			if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			record.ID = record.Name + "-" + record.Type
			records[record.ID] = record
			result = record
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer provider.Close()
	r := newRigAt(t, "http://test", Options{DNSBase: provider.URL})
	ctx := context.Background()
	call := func(method, path string, body any) []byte {
		t.Helper()
		code, b, _ := r.c.do(method, path, body, nil)
		if code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, code, b)
		}
		return b
	}
	if err := r.st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "fixture", AutoDNS: true}); err != nil {
		t.Fatal(err)
	}
	nodePath := "/api/admin/nodes/" + itoa(r.nodeID)
	call("PATCH", nodePath, map[string]any{"Name": "NAT server", "PublicAddr": "192.0.2.10"})
	body := map[string]any{"Name": "NAT", "Kind": "nat", "EntryAddressSource": "server_ipv4", "PortFrom": 20000, "PortTo": 20009, "PortOffset": 10000}
	g := mustJSON[struct {
		Ingress store.Ingress `json:"ingress"`
	}](t, call("POST", nodePath+"/ingresses", body)).Ingress
	if g.EntryHost != "192.0.2.10" {
		t.Fatal(g)
	}
	ib := mustJSON[domain.Inbound](t, call("POST", nodePath+"/inbounds", map[string]any{"Tag": "inherited", "Protocol": "mieru", "Port": 20001, "IngressID": g.ID}))
	e := mustJSON[domain.Entry](t, call("POST", "/api/admin/entries", map[string]any{"Name": "Follow ingress", "InboundID": ib.ID}))
	manual := mustJSON[domain.Entry](t, call("POST", "/api/admin/entries", map[string]any{"Name": "Manual endpoint", "InboundID": ib.ID, "DisplayHost": "203.0.113.30", "DisplayPort": 443}))
	if !e.FollowIngress || e.DisplayHost != g.EntryHost || e.DisplayPort != 30001 || manual.FollowIngress {
		t.Fatal(e, manual)
	}
	body["EntryDomain"], body["PortOffset"] = "NAT.Example.Com.", 20000
	call("PATCH", "/api/admin/ingresses/"+itoa(g.ID), body)
	got := mustJSON[[]domain.Entry](t, call("GET", "/api/admin/entries", nil))
	found := false
	for _, entry := range got {
		if entry.ID == e.ID {
			found = true
			if !entry.FollowIngress || entry.DisplayHost != "nat.example.com" || entry.DisplayPort != 40001 {
				t.Fatal(entry)
			}
		}
		if entry.ID == manual.ID && (entry.DisplayHost != manual.DisplayHost || entry.DisplayPort != manual.DisplayPort || entry.FollowIngress) {
			t.Fatal("manual endpoint changed", entry)
		}
	}
	if !found {
		t.Fatal("following entry missing")
	}
	response := mustJSON[map[string]any](t, call("PATCH", nodePath, map[string]any{"Name": "NAT server", "PublicAddr": "192.0.2.11"}))
	if response["ingress_dns_pending"] != true {
		t.Fatal("DNS work not reported", response)
	}
	r.web.NodeAddresses().Tick(ctx, time.Now())
	mu.Lock()
	record := records["nat.example.com-A"]
	mu.Unlock()
	if record.Content != "192.0.2.11" || record.Proxied {
		t.Fatal("DNS did not follow inherited address", record)
	}
	current, err := r.st.IngressByID(ctx, g.ID)
	if err != nil || current.EntryHost != "192.0.2.11" || current.EntryAddressSource != "server_ipv4" {
		t.Fatal(current, err)
	}
}
