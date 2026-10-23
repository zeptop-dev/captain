package service

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
)

func dnsTestStore(t *testing.T) (*store.Store, *domain.Node) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "dns.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	n := &domain.Node{Name: "<edge>", Domain: "node.example.com", PublicAddr: "192.0.2.10"}
	if err := st.CreateNode(ctx, n, "dns-test", time.Hour); err != nil {
		t.Fatal(err)
	}
	return st, n
}

func TestDNSHealthViewsAndResolverClassification(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	g := &store.Ingress{NodeID: n.ID, Name: "NAT", Kind: "nat", BindIP: "10.10.0.2", LineIP: "10.10.0.2", EntryDomain: "nat.example.com", EntryHost: "203.0.113.30"}
	if err := st.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	d := &DNSHealth{Store: st, Lookup: func(_ context.Context, _ string, host string) ([]string, error) {
		if host == "nat.example.com" {
			return []string{g.EntryHost}, nil
		}
		return []string{n.PublicAddr}, nil
	}}
	h, err := d.Check(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Targets) != 2 || h.Targets[0].Status != "ok" || h.Targets[1].Status != "ok" || !slices.Equal(h.Targets[1].Expected, []string{g.EntryHost}) {
		t.Fatalf("NAT expectation: %+v", h)
	}
	n.PublicAddr = "198.51.100.20"
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.Snapshot(ctx, n.ID)
	if err != nil || !fresh.Stale || fresh.CheckedAt != 0 {
		t.Fatalf("old config exposed: %+v %v", fresh, err)
	}
	cases := []struct {
		name, mode string
		a, b       store.DNSAnswer
		want       string
	}{
		{"both match", "direct", store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, "ok"},
		{"agreed wrong", "direct", store.DNSAnswer{Addresses: []string{"198.51.100.20"}}, store.DNSAnswer{Addresses: []string{"198.51.100.20"}}, "mismatch"},
		{"missing v6", "direct", store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, "mismatch"},
		{"disagree", "direct", store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, store.DNSAnswer{Addresses: []string{"198.51.100.20"}}, "inconsistent"},
		{"missing", "direct", store.DNSAnswer{Error: "not_found"}, store.DNSAnswer{Error: "not_found"}, "missing"},
		{"timeout is unknown", "direct", store.DNSAnswer{Error: "timeout"}, store.DNSAnswer{Error: "not_found"}, "unknown"},
		{"partial nxdomain", "direct", store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, store.DNSAnswer{Error: "not_found"}, "inconsistent"},
		{"geo CDN allowed", "shared", store.DNSAnswer{Addresses: []string{"192.0.2.10"}}, store.DNSAnswer{Addresses: []string{"198.51.100.20"}}, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := []string{"192.0.2.10"}
			if tc.name == "missing v6" {
				expected = append(expected, "2001:db8::10")
			}
			target := store.DNSTarget{Mode: tc.mode, Expected: expected, Answers: []store.DNSAnswer{tc.a, tc.b}}
			if got := dnsTargetStatus(target); got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
	d.Lookup = func(_ context.Context, source, _ string) ([]string, error) {
		if source == "panel" {
			return nil, &net.DNSError{IsNotFound: true}
		}
		return nil, context.DeadlineExceeded
	}
	answers := d.resolve(ctx, n.Domain)
	if answers[0].Error != "not_found" || answers[1].Error != "timeout" {
		t.Fatal(answers)
	}
	n.DomainShared = true
	if err := st.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	h, err = d.Snapshot(ctx, n.ID)
	if err != nil || h.Targets[0].Mode != "shared" || len(h.Targets[0].Expected) != 0 {
		t.Fatalf("shared %+v %v", h, err)
	}
}

func TestDNSHealthSustainedAlertsRestartUnknownAndRecovery(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	settings := store.ProbeSettings{Enabled: true}
	if err := st.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	sent := []string{}
	p := &Probe{Store: st, Notify: &notify.Notifier{}, AlertWindow: -1}
	p.adminSend = func(_ context.Context, s string) { sent = append(sent, s) }
	at := time.Now()
	mode := "bad"
	d := &DNSHealth{Store: st, Probe: p, Now: func() time.Time { return at }, Lookup: func(_ context.Context, source, _ string) ([]string, error) {
		switch mode {
		case "unknown":
			return nil, context.DeadlineExceeded
		case "ok":
			return []string{n.PublicAddr}, nil
		case "inconsistent":
			if source == "panel" {
				return []string{n.PublicAddr}, nil
			}
		}
		return []string{"198.51.100.20"}, nil
	}}
	check := func() *store.DNSHealth {
		t.Helper()
		h, err := d.Check(ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if h := check(); h.FailureCount != 1 {
		t.Fatal(h)
	}
	at = at.Add(time.Minute)
	if h := check(); h.FailureCount != 1 {
		t.Fatal("manual checks accelerated alerts")
	}
	at = at.Add(4 * time.Minute)
	if h := check(); h.FailureCount != 2 || len(sent) != 0 {
		t.Fatalf("early alert %+v %v", h, sent)
	}
	// A new service instance retains the durable failure window.
	d = &DNSHealth{Store: st, Probe: p, Now: d.Now, Lookup: d.Lookup}
	at = at.Add(5 * time.Minute)
	if h := check(); h.FailureCount != 3 || len(sent) != 1 {
		t.Fatalf("missing alert %+v %v", h, sent)
	}
	if !strings.Contains(sent[0], "&lt;edge&gt;") {
		t.Fatal("unescaped node in notification", sent)
	}
	for _, m := range []string{"unknown", "inconsistent"} {
		mode = m
		at = at.Add(5 * time.Minute)
		check()
		rows, err := st.MonitorIncidents(ctx, n.ID, 0, "open")
		if err != nil || len(rows) != 1 || len(sent) != 1 {
			t.Fatalf("unknown recovered: %v %v %v", rows, sent, err)
		}
	}
	mode = "ok"
	at = at.Add(5 * time.Minute)
	check()
	rows, err := st.MonitorIncidents(ctx, n.ID, 0, "resolved")
	if err != nil || len(rows) != 1 || rows[0].Resolution != "recovered" || len(sent) != 2 {
		t.Fatalf("recovery: %v %v %v", rows, sent, err)
	}
}

func TestDNSHealthRejectsLateConfigurationAndConcurrentCheck(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d := &DNSHealth{Store: st, Lookup: func(ctx context.Context, _, _ string) ([]string, error) {
		once.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return []string{n.PublicAddr}, nil
		}
	}}
	done := make(chan error, 1)
	go func() { _, err := d.Check(ctx, n.ID); done <- err }()
	<-started
	if _, err := d.Check(ctx, n.ID); !errors.Is(err, ErrDNSBusy) {
		t.Fatal("concurrent check", err)
	}
	st.Topology.Lock()
	n.Domain = "renamed.example.com"
	err := st.UpdateNode(ctx, n)
	st.Topology.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrDNSConfigChanged) {
		t.Fatal("stale result accepted", err)
	}
	if _, err := st.DNSHealth(ctx, n.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("stale snapshot persisted", err)
	}
}

func TestDNSHealthDoesNotTreatPanelDowntimeAsContinuousFailure(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	at := time.Now()
	d := &DNSHealth{Store: st, Now: func() time.Time { return at }, Lookup: func(context.Context, string, string) ([]string, error) { return []string{"198.51.100.20"}, nil }}
	if _, err := d.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Hour)
	h, err := d.Check(ctx, n.ID)
	if err != nil || h.FailureCount != 1 || h.FailureSince != at.Unix() {
		t.Fatalf("downtime counted: %+v %v", h, err)
	}
}

func TestDNSHealthSilenceDelaysNotificationAndDisableEndsIncident(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	at := time.Now()
	if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	window := store.MonitorWindow{NodeID: &n.ID, Kind: "silence", StartsAt: at.Unix(), EndsAt: at.Add(12 * time.Minute).Unix()}
	if err := st.CreateMonitorWindow(ctx, &window, at); err != nil {
		t.Fatal(err)
	}
	sent := []string{}
	p := &Probe{Store: st, Notify: &notify.Notifier{}, AlertWindow: -1}
	p.adminSend = func(_ context.Context, s string) { sent = append(sent, s) }
	d := &DNSHealth{Store: st, Probe: p, Now: func() time.Time { return at }, Lookup: func(context.Context, string, string) ([]string, error) { return []string{"198.51.100.20"}, nil }}
	for i := 0; i < 3; i++ {
		if _, err := d.Check(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
		at = at.Add(5 * time.Minute)
	}
	rows, err := st.MonitorIncidents(ctx, n.ID, 0, "open")
	if err != nil || len(rows) != 1 || rows[0].NotifiedAt != 0 || len(sent) != 0 {
		t.Fatalf("silence lost incident or sent notice: %v %v %v", rows, sent, err)
	}
	if _, err := d.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatal("no delayed notification", sent)
	}
	if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	p.Invalidate()
	at = at.Add(5 * time.Minute)
	if _, err := d.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = st.MonitorIncidents(ctx, n.ID, 0, "resolved")
	if err != nil || len(rows) != 1 || rows[0].Resolution != "disabled" || len(sent) != 1 {
		t.Fatalf("disable emitted recovery: %v %v %v", rows, sent, err)
	}
}

func TestDNSHealthStorageFailureDoesNotResolveIncident(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CheckIncident(ctx, n.ID, store.IncidentCheck{Kind: "dns", Active: true}, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`CREATE TRIGGER fail_dns_snapshot BEFORE INSERT ON node_dns_health BEGIN SELECT RAISE(FAIL, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	d := &DNSHealth{Store: st, Probe: &Probe{Store: st}, Lookup: func(context.Context, string, string) ([]string, error) { return []string{n.PublicAddr}, nil }}
	if _, err := d.Check(ctx, n.ID); err == nil {
		t.Fatal("ignored storage failure")
	}
	rows, err := st.MonitorIncidents(ctx, n.ID, 0, "open")
	if err != nil || len(rows) != 1 {
		t.Fatalf("storage failure recovered incident: %v %v", rows, err)
	}
}

func TestDNSHealthBackwardClockStartsNewObservation(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	at := time.Now()
	d := &DNSHealth{Store: st, Now: func() time.Time { return at }, Lookup: func(context.Context, string, string) ([]string, error) { return []string{"198.51.100.20"}, nil }}
	if _, err := d.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	at = at.Add(-time.Hour)
	stale, err := d.Snapshot(ctx, n.ID)
	if err != nil || !stale.Stale {
		t.Fatalf("future observation not stale %+v %v", stale, err)
	}
	h, err := d.Check(ctx, n.ID)
	if err != nil || h.CheckedAt != at.Unix() || h.FailureSince != at.Unix() || h.FailureCount != 1 {
		t.Fatalf("reused future baseline %+v %v", h, err)
	}
}

func TestDNSPropagationRequiresCurrentSuccessfulWrite(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now()
	answers := []string{"198.51.100.20"}
	health := &DNSHealth{Store: st, Now: func() time.Time { return at }, Lookup: func(context.Context, string, string) ([]string, error) { return answers, nil }}
	write := func(outcome, ip string) {
		rec := dns.Record{ID: "record", Type: "A", Name: n.Domain, Content: ip, TTL: 600}
		change := store.DNSChange{NodeID: &n.ID, Name: n.Domain, Source: "node_address", Wanted: rec}
		must(st.BeginDNSChange(ctx, &change))
		must(st.FinishDNSChange(ctx, change.ID, outcome, &rec))
		// The store timestamps the write with the real clock. Keep the injected
		// observation clock at or after it even when race instrumentation makes
		// the write cross a second boundary.
		at = time.Now()
	}
	check := func(want bool) {
		t.Helper()
		h, err := health.Snapshot(ctx, n.ID)
		must(err)
		if len(h.Targets) != 1 || h.Targets[0].PropagationPending != want {
			t.Fatalf("propagation=%v want %v", h.Targets, want)
		}
		if (dnsCheckInterval(h) == 30*time.Second) != want {
			t.Fatal("unexpected recheck interval")
		}
	}
	_, err := health.Check(ctx, n.ID)
	must(err)
	check(false) // no history is not success
	write("success", n.PublicAddr)
	check(true)
	write("failed", n.PublicAddr)
	check(false) // do not resurrect older success
	write("success", "203.0.113.30")
	check(false) // old configuration must not leak
	write("success", n.PublicAddr)
	check(true)
	at = at.Add(time.Minute)
	answers = []string{n.PublicAddr}
	_, err = health.Check(ctx, n.ID)
	must(err)
	check(false) // stop accelerated checks once both resolvers agree
	at = at.Add(time.Minute)
	answers = []string{"198.51.100.20"}
	_, err = health.Check(ctx, n.ID)
	must(err)
	check(true)
	at = at.Add(10 * time.Minute)
	check(false) // never mask a persistent failure
}
