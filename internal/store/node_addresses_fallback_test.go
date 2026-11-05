package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestFallbackAddressNeverPromotesEntryOrDNS(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "fallback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "restricted", PublicAddr: "198.51.100.20"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateNodeWithAddressPolicy(ctx, n, "pair", time.Hour, true, true))
	_, err = s.RedeemPairCode(ctx, "pair", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Truncate(time.Second)
	for _, offset := range []time.Duration{0, 30 * time.Second, 5 * time.Minute, 10 * time.Minute} {
		ts := at.Add(offset)
		f := &agentproto.PublicAddressFallback{Address: "203.0.113.30", Source: "captain_cloudflare_trace", Origin: "https://panel.example.com:2096", SampledAt: ts.Unix()}
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: ts.Unix(), FallbackIPv4: f}, ts))
		got, pending, err := s.ReconcileNodeAddresses(ctx, n.ID, ts)
		must(err)
		if pending || got.PublicAddr != n.PublicAddr || got.V6Addr != "" {
			t.Fatalf("advisory promoted entry/DNS: %+v pending=%v", got, pending)
		}
	}
	got, err := s.NodeAddresses(ctx, n.ID)
	must(err)
	if got.Observed.IPv4.Address != "" || got.Observed.IPv4.ConfirmedAt != 0 || got.Observed.FallbackIPv4 == nil || got.Observed.FallbackIPv4.Address != "203.0.113.30" {
		t.Fatalf("evidence conflated %+v", got)
	}
	// Duplicate reports cannot renew candidate freshness.
	last := got.Observed.FallbackIPv4.ReceivedAt
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Add(10 * time.Minute).Unix()}, at.Add(11*time.Minute)))
	got, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if got.Observed.FallbackIPv4.ReceivedAt != last {
		t.Fatal("duplicate renewed advisory")
	}
	// A bad optional advisory must not block valid authoritative IPv6.
	ts := at.Add(12 * time.Minute)
	bad := &agentproto.PublicAddressFallback{Address: "203.0.113.30", Source: "captain_cloudflare_trace", Origin: "https://user:secret@panel.example.com:2096", SampledAt: ts.Unix()}
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: ts.Unix(), IPv6: "2001:db8::20", FallbackIPv4: bad}, ts))
	got, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if got.Observed.FallbackIPv4 != nil || got.Observed.IPv6.Address != "2001:db8::20" {
		t.Fatalf("invalid advisory blocked valid observation %+v", got)
	}
	// Re-pairing clears advisory with all other identity-bound observations.
	must(s.ResetPairCode(ctx, n.ID, "again", time.Hour))
	_, err = s.RedeemPairCode(ctx, "again", "new-token", "node", "test", "linux")
	must(err)
	got, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if got.Observed.FallbackIPv4 != nil || got.Observed.ReceivedAt != 0 {
		t.Fatal("re-pair kept stale observation")
	}
}

func TestFallbackValidationIsBoundedAndFamilySpecific(t *testing.T) {
	at := time.Now()
	base := agentproto.PublicAddressFallback{Address: "192.0.2.10", Source: "captain_cloudflare_trace", Origin: "https://panel.example.com:2096", SampledAt: at.Unix()}
	if addressFallback(&base, at.Unix(), at, false) == nil {
		t.Fatal("valid candidate rejected")
	}
	cases := []func(*agentproto.PublicAddressFallback){
		func(f *agentproto.PublicAddressFallback) { f.Origin = "https://panel.example.com:0" },
		func(f *agentproto.PublicAddressFallback) { f.Origin = "https://panel.example.com:65536" },
		func(f *agentproto.PublicAddressFallback) { f.Origin = "https://panel.example.com:" },
		func(f *agentproto.PublicAddressFallback) { f.Address = "10.10.0.2" },
		func(f *agentproto.PublicAddressFallback) { f.Address = "2001:db8::20" },
		func(f *agentproto.PublicAddressFallback) { f.Error = "timeout" },
		func(f *agentproto.PublicAddressFallback) { f.Source = "untrusted" },
		func(f *agentproto.PublicAddressFallback) { f.Origin = "http://panel.example.com" },
		func(f *agentproto.PublicAddressFallback) { f.Origin += "/?token=private" },
		func(f *agentproto.PublicAddressFallback) { f.SampledAt -= 61 },
		func(f *agentproto.PublicAddressFallback) { f.SampledAt += 2 },
	}
	for i, change := range cases {
		f := base
		change(&f)
		if addressFallback(&f, at.Unix(), at, false) != nil {
			t.Fatalf("accepted invalid candidate %d", i)
		}
	}
	base.Address = ""
	base.Error = "timeout"
	if addressFallback(&base, at.Unix(), at, false) == nil {
		t.Fatal("bounded failure lost")
	}
	base.Error = "arbitrary server response"
	if addressFallback(&base, at.Unix(), at, false) != nil {
		t.Fatal("unbounded failure retained")
	}
}
