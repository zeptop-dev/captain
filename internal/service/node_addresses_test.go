package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeAddressesDNSOnlyFollowsOptedInFamily(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	dom := store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test-token", AutoDNS: true}
	must(st.CreateDomain(ctx, &dom))
	ingress := &store.Ingress{NodeID: n.ID, Name: "NAT", Kind: "nat", BindIP: "10.10.0.2", EntryHost: "203.0.113.30", EntryDomain: "nat.example.com"}
	must(st.CreateIngress(ctx, ingress))
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		if r.URL.Path == "/zones" {
			result = []map[string]string{{"id": "zone"}}
		} else if r.Method == "GET" {
			result = []dns.Record{{ID: "record", Name: n.Domain, Type: "A", Content: n.PublicAddr, TTL: 600}}
		} else {
			writes.Add(1)
			var rec dns.Record
			must(json.NewDecoder(r.Body).Decode(&rec))
			if rec.Type != "A" || rec.Content != "198.51.100.20" || rec.Name != n.Domain {
				t.Errorf("wrong automatic record %+v", rec)
			}
			// Intent must already exist before provider I/O; the worker must not hold
			// Accounts across it, otherwise reset/renumber can stall all new readers.
			history, err := st.DNSChanges(ctx, n.ID, 0)
			if err != nil || len(history) != int(writes.Load()) || history[0].Outcome != "unknown" {
				t.Errorf("missing intent: %v %v", history, err)
			}
			if !st.Accounts.TryLock() {
				t.Error("network call held Accounts")
			} else {
				st.Accounts.Unlock()
			}
			if writes.Load() == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			rec.ID = "record"
			result = rec
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer srv.Close()
	worker := &NodeAddresses{Store: st, DNS: &DNS{Store: st, Base: srv.URL}}
	at := time.Now().Truncate(time.Second)
	for _, offset := range []time.Duration{0, 5 * time.Minute} {
		when := at.Add(offset)
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: when.Unix(), IPv4: "198.51.100.20", IPv6: "2001:db8::10"}, when))
	}
	worker.Tick(ctx, at.Add(5*time.Minute))
	if writes.Load() != 0 {
		t.Fatal("manual entry wrote DNS")
	}
	must(st.SetAddressPolicy(ctx, n.ID, true, false))
	worker.Tick(ctx, at.Add(5*time.Minute))
	if writes.Load() != 1 {
		t.Fatalf("writes %d", writes.Load())
	}
	got, err := st.NodeAddresses(ctx, n.ID)
	must(err)
	if got.EntryIPv4 != "198.51.100.20" || got.EntryIPv6 != "" || !got.DNSPending {
		t.Fatalf("result %+v", got)
	}
	worker.Tick(ctx, at.Add(6*time.Minute))
	if writes.Load() != 1 {
		t.Fatal("DNS failure retried before five minutes")
	}
	worker.Tick(ctx, at.Add(10*time.Minute))
	if writes.Load() != 2 {
		t.Fatalf("pending DNS not retried: %d", writes.Load())
	}
	got, err = st.NodeAddresses(ctx, n.ID)
	must(err)
	if got.DNSPending {
		t.Fatal("successful retry left pending flag")
	}
	worker.Tick(ctx, at.Add(11*time.Minute))
	if writes.Load() != 2 {
		t.Fatal("successful DNS repeated unchanged write")
	}
	ingresses, err := st.IngressesByNode(ctx, n.ID)
	must(err)
	if len(ingresses) != 1 || ingresses[0].EntryHost != "203.0.113.30" {
		t.Fatal("NAT public entry changed")
	}
}

// New IPv4-only nodes must create their first A record without waiting for IPv6.
func TestNodeAddressesInitialIPv4OnlyDNS(t *testing.T) {
	ctx := context.Background()
	st, _ := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	n := &domain.Node{Name: "new", Domain: "new.example.com"}
	must(st.CreateNodeWithAddressPolicy(ctx, n, "new-pair", time.Hour, true, true))
	_, err := st.RedeemPairCode(ctx, "new-pair", "token", "new", "test", "linux")
	must(err)
	dom := store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "test-token", AutoDNS: true}
	must(st.CreateDomain(ctx, &dom))
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case r.URL.Path == "/zones":
			result = []map[string]string{{"id": "zone"}}
		case r.Method == "GET":
			result = []dns.Record{}
		default:
			writes.Add(1)
			var rec dns.Record
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				t.Error(err)
			}
			if r.Method != "POST" || rec.Type != "A" || rec.Content != "198.51.100.20" || rec.Name != n.Domain {
				t.Errorf("unexpected initial DNS write: %s %+v", r.Method, rec)
			}
			rec.ID = "record"
			result = rec
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer srv.Close()
	var checkMu sync.Mutex
	checks := map[string]int{}
	health := &DNSHealth{Store: st, LookupFamily: func(_ context.Context, source, host, family string) ([]string, error) {
		checkMu.Lock()
		checks[source+"/"+family]++
		checkMu.Unlock()
		if host != n.Domain {
			t.Errorf("unexpected health target %q", host)
		}
		if family == "ip4" {
			return []string{"198.51.100.20"}, nil
		}
		if family != "ip6" {
			t.Errorf("unexpected health query family %q", family)
		}
		return nil, &net.DNSError{IsNotFound: true}
	}}
	worker := &NodeAddresses{Store: st, DNS: &DNS{Store: st, Base: srv.URL}, Health: health}
	at := time.Now().Truncate(time.Second)
	sample := func(when time.Time) {
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: when.Unix(), IPv4: "198.51.100.20"}, when))
	}
	sample(at)
	worker.Tick(ctx, at)
	if writes.Load() != 0 {
		t.Fatal("unconfirmed address wrote DNS")
	}
	sample(at.Add(time.Minute))
	worker.Tick(ctx, at.Add(5*time.Minute))
	got, err := st.NodeAddresses(ctx, n.ID)
	must(err)
	checkMu.Lock()
	defer checkMu.Unlock()
	if len(checks) != 4 {
		t.Fatalf("independent health lookups: %v", checks)
	}
	for _, source := range []string{"panel", "cloudflare"} {
		for _, family := range []string{"ip4", "ip6"} {
			if checks[source+"/"+family] != 1 {
				t.Errorf("expected one %s/%s query: %v", source, family, checks)
			}
		}
	}
	h, err := st.DNSHealth(ctx, n.ID)
	must(err)
	if len(h.Targets) != 1 || h.Targets[0].Status != "ok" || len(h.Targets[0].Families) != 2 || h.Targets[0].Families[0].Status != "ok" || h.Targets[0].Families[1].Status != "unconfigured" {
		t.Fatalf("IPv4-only initial DNS health: %+v", h)
	}
	if writes.Load() != 1 || got.EntryIPv4 != "198.51.100.20" || got.EntryIPv6 != "" || got.DNSPending {
		t.Fatalf("initial sync: writes=%d addresses=%+v", writes.Load(), got)
	}
}

func TestNodeAddressNotificationsAreBoundedAndNonblocking(t *testing.T) {
	worker := &NodeAddresses{}
	for range 1000 {
		worker.Notify(1)
	}
	if len(worker.notifications()) != 256 {
		t.Fatal("unexpected notification bound")
	}
	var absent *NodeAddresses
	absent.Notify(1) // optional dependency in old API fixtures
}

func TestNodeAddressNotificationAppliesWithoutPeriodicSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	must(st.SetAddressPolicy(ctx, n.ID, true, false))
	worker := &NodeAddresses{Store: st}
	worker.Notify(0) // sentinel consumed only after the startup sweep
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for len(worker.notifications()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(worker.notifications()) != 0 {
		t.Fatal("worker did not start")
	}
	for _, ago := range []time.Duration{3 * time.Minute, time.Minute} {
		at := time.Now().Add(-ago)
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: "198.51.100.20"}, at))
	}
	worker.Notify(n.ID)
	for time.Now().Before(deadline) {
		got, err := st.NodeByID(ctx, n.ID)
		must(err)
		if got.PublicAddr == "198.51.100.20" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("address update waited for the one-minute sweep")
}
