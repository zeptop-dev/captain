package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestAutomaticDNSPreservesIngressTargets(t *testing.T) {
	for _, tc := range []struct {
		name, otherDomain, otherIP, requestedIP string
		blocked                                 bool
	}{
		{"same-target", "ENTRY.EXAMPLE.COM.", "198.51.100.20", "198.51.100.20", false},
		{"different-ingress", "entry.example.com", "203.0.113.30", "198.51.100.20", true},
		{"direct-ipv6-bypass", "entry.example.com", "198.51.100.20", "2001:db8::20", true},
		{"missing-target", "entry.example.com", "", "198.51.100.20", true},
		{"different-hostname", "other.example.com", "203.0.113.30", "198.51.100.20", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, n := dnsTestStore(t)
			if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "fixture", AutoDNS: true}); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateIngress(ctx, &store.Ingress{NodeID: n.ID, Name: "existing", EntryDomain: tc.otherDomain, EntryHost: tc.otherIP, PortFrom: 10000, PortTo: 11000}); err != nil {
				t.Fatal(err)
			}
			provider := &dnsSyncProvider{records: map[string][]dns.Record{}}
			srv := httptest.NewServer(provider)
			defer srv.Close()
			d := &DNS{Store: st, Base: srv.URL}
			result := d.Ensure(ctx, "entry.example.com", tc.requestedIP)
			if tc.blocked {
				if result.Reason != "domain_address_conflict" || result.Action != "skipped" || provider.writes != 0 {
					t.Fatalf("overwrote an ingress target: %+v, writes=%d", result, provider.writes)
				}
			} else if result.Error != "" || result.Reason != "" || provider.writes != 1 {
				t.Fatalf("valid target was not written: %+v, writes=%d", result, provider.writes)
			}
		})
	}
}

func TestManualDNSCannotAddDirectFamilyToIngressDomain(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	n.V6Addr = "2001:db8::20"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDomain(ctx, &store.Domain{Name: "example.com", Provider: "cloudflare", CFToken: "fixture", AutoDNS: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateIngress(ctx, &store.Ingress{NodeID: n.ID, Name: "IPv4 ingress", EntryDomain: n.Domain, EntryHost: n.PublicAddr, PortFrom: 10000, PortTo: 11000}); err != nil {
		t.Fatal(err)
	}
	d := &DNS{Store: st}
	p, err := d.PrepareNodeSync(ctx, n.ID)
	if err != nil || p.Enabled || p.Reason != "domain_address_conflict" {
		t.Fatalf("manual synchronization could bypass IPv4 ingress: %+v %v", p, err)
	}
	// A server and ingress intentionally sharing the same single target remain
	// compatible; removing the unrelated direct family restores eligibility.
	n.V6Addr = ""
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	p, err = d.PrepareNodeSync(ctx, n.ID)
	if err != nil || !p.Enabled {
		t.Fatalf("identical targets refused: %+v %v", p, err)
	}
}
