package service

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestDNSFamiliesAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name, v6, status, aggregate string
		expected                    bool
	}{
		{"both healthy", "2001:db8::20", "ok", "ok", true},
		{"missing registered v6", "", "missing", "missing", true},
		{"v6 timeout", "timeout", "unknown", "unknown", true},
		{"missing optional v6", "", "unconfigured", "ok", false},
		{"unexpected v6", "2001:db8::30", "unexpected", "inconsistent", false},
		{"wrong v6", "2001:db8::30", "mismatch", "mismatch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, n := dnsTestStore(t)
			if tc.expected {
				n.V6Addr = "2001:db8::20"
				if err := st.UpdateNode(ctx, n); err != nil {
					t.Fatal(err)
				}
			}
			d := &DNSHealth{Store: st, LookupFamily: func(_ context.Context, _, _ string, family string) ([]string, error) {
				if family == "ip4" {
					return []string{n.PublicAddr}, nil
				}
				if family != "ip6" {
					t.Errorf("combined family query %q", family)
				}
				if tc.v6 == "timeout" {
					return nil, context.DeadlineExceeded
				}
				if tc.v6 == "" {
					return nil, &net.DNSError{IsNotFound: true}
				}
				return []string{tc.v6}, nil
			}}
			h, err := d.Check(ctx, n.ID)
			if err != nil {
				t.Fatal(err)
			}
			target := h.Targets[0]
			if target.Families[0].Status != "ok" || target.Families[1].Status != tc.status || target.Status != tc.aggregate {
				t.Fatalf("independent family evidence %+v", target)
			}
			if target.Answers[0].Addresses[0] != n.PublicAddr {
				t.Fatal("successful IPv4 discarded by IPv6 failure")
			}
			if tc.v6 == "timeout" && target.Families[1].Answers[0].Error != "timeout" {
				t.Fatal("family error absent")
			}
		})
	}
}
func TestDNSFamiliesSiblingErrorsDoNotMaskKnownFailure(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	n.V6Addr = "2001:db8::20"
	_ = st.UpdateNode(ctx, n)
	d := &DNSHealth{Store: st, LookupFamily: func(_ context.Context, _, _ string, family string) ([]string, error) {
		if family == "ip4" {
			return []string{"198.51.100.20"}, nil
		}
		return nil, context.DeadlineExceeded
	}}
	h, err := d.Check(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Targets[0].Status != "mismatch" || h.Targets[0].Families[1].Status != "unknown" || h.FailureCount != 1 {
		t.Fatalf("masked known failure %+v", h)
	}
}
func TestDNSCheckRejectsResultsBeforeProviderWrite(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d := &DNSHealth{Store: st, LookupFamily: func(_ context.Context, _, _ string, _ string) ([]string, error) {
		once.Do(func() { close(entered) })
		<-release
		return []string{n.PublicAddr}, nil
	}}
	done := make(chan error, 1)
	go func() { _, err := d.Check(ctx, n.ID); done <- err }()
	<-entered
	rec := dns.Record{Name: n.Domain, Type: "A", Content: n.PublicAddr}
	c := store.DNSChange{NodeID: &n.ID, Name: n.Domain, Wanted: rec}
	if err := st.BeginDNSChange(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishDNSChange(ctx, c.ID, "success", &rec); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrDNSConfigChanged) {
		t.Fatalf("late result survived write: %v", err)
	}
	snapshot, err := d.Snapshot(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Stale || snapshot.CheckedAt != 0 {
		t.Fatalf("old query became fresh: %+v", snapshot)
	}
}
func TestDNSFamilyPropagationOnlyMarksWrittenFamily(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	n.V6Addr = "2001:db8::20"
	_ = st.UpdateNode(ctx, n)
	d := &DNSHealth{Store: st, LookupFamily: func(context.Context, string, string, string) ([]string, error) {
		return nil, &net.DNSError{IsNotFound: true}
	}}
	if _, err := d.Check(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	rec := dns.Record{Name: n.Domain, Type: "AAAA", Content: n.V6Addr}
	c := store.DNSChange{NodeID: &n.ID, Name: n.Domain, Wanted: rec}
	if err := st.BeginDNSChange(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishDNSChange(ctx, c.ID, "success", &rec); err != nil {
		t.Fatal(err)
	}
	h, err := d.Snapshot(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Stale || h.Targets[0].Families[0].PropagationPending || !h.Targets[0].Families[1].PropagationPending {
		t.Fatalf("wrong propagation families %+v", h.Targets[0])
	}
	d.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	h, _ = d.Snapshot(ctx, n.ID)
	if h.Targets[0].PropagationPending {
		t.Fatal("expired propagation")
	}
}
