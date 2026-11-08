package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestIngressAddressDNSRetriesPersistedCurrentIntentForUnpairedNode(t *testing.T) {
	for _, source := range []string{store.IngressAddressIPv4, "custom_nat", "custom_iplc"} {
		t.Run(source, func(t *testing.T) { testIngressDNSRetry(t, source) })
	}
}
func testIngressDNSRetry(t *testing.T, source string) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	dom := &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test-token", AutoDNS: true}
	if err := st.CreateDomain(ctx, dom); err != nil {
		t.Fatal(err)
	}
	g := &store.Ingress{NodeID: n.ID, Name: "NAT", Kind: "nat", EntryAddressSource: store.IngressAddressIPv4, EntryDomain: "nat.example.com"}
	if source != store.IngressAddressIPv4 {
		g.EntryAddressSource, g.EntryHost = store.IngressAddressCustom, n.PublicAddr
		if source == "custom_iplc" {
			g.Kind = "iplc"
		}
	}
	if err := st.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	current := "198.51.100.20"
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case r.URL.Path == "/zones":
			result = []map[string]string{{"id": "zone"}}
		case r.Method == http.MethodGet:
			result = []dns.Record{{ID: "record", Name: g.EntryDomain, Type: "A", Content: current, TTL: 600}}
		default:
			var rec dns.Record
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				t.Error(err)
			}
			if rec.Name != g.EntryDomain || rec.Type != "A" {
				t.Error("wrong ingress DNS target", rec)
			}
			if writes.Add(1) == 1 {
				w.WriteHeader(503)
				return
			}
			if rec.Content != "203.0.113.30" {
				t.Error("retry published stale address", rec)
			}
			current = rec.Content
			rec.ID = "record"
			result = rec
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer cf.Close()
	worker := &NodeAddresses{Store: st, DNS: &DNS{Store: st, Base: cf.URL}}
	at := time.Now().Truncate(time.Second)
	worker.Tick(ctx, at)
	if writes.Load() != 1 {
		t.Fatal("unpaired node ingress was not synchronized", writes.Load())
	}
	worker.Tick(ctx, at.Add(time.Minute))
	if writes.Load() != 1 {
		t.Fatal("retry backoff ignored")
	}
	n.PublicAddr = "203.0.113.30"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if source != store.IngressAddressIPv4 {
		unchanged, err := st.IngressByID(ctx, g.ID)
		if err != nil || unchanged.EntryHost != g.EntryHost {
			t.Fatal("server edit changed a custom ingress", unchanged, err)
		}
		g.EntryHost = "203.0.113.30"
		if err := st.UpdateIngress(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	// A new process must pick up the latest desired address, not an old queued IP.
	worker = &NodeAddresses{Store: st, DNS: &DNS{Store: st, Base: cf.URL}}
	worker.Tick(ctx, at.Add(2*time.Minute))
	if writes.Load() != 2 || current != "203.0.113.30" {
		t.Fatal(writes.Load(), current)
	}
	pending, err := st.PendingIngressDNS(ctx, n.ID, at.Add(10*time.Minute))
	if err != nil || len(pending) != 0 {
		t.Fatal("successful DNS stayed pending", pending, err)
	}
	worker.Tick(ctx, at.Add(20*time.Minute))
	if writes.Load() != 2 {
		t.Fatal("completed DNS was replayed")
	}
	history, err := st.DNSChanges(ctx, n.ID, 0)
	if err != nil || len(history) != 2 {
		t.Fatal("DNS history lost retry", history, err)
	}
}
