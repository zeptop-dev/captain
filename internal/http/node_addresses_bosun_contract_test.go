package http

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/store"
)

// Opt-in integration consumes actual bosun TLS/driver output over authenticated
// HTTP and waits the production confirmation window without backdating storage.
func TestFallbackAddressBosunWireHTTP(t *testing.T) {
	path := os.Getenv("BOSUN_FALLBACK_CONTRACT_FILE")
	if path == "" {
		t.Skip("set BOSUN_FALLBACK_CONTRACT_FILE to the bosun TLS/driver fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var reports []agentproto.Report
	if err := json.Unmarshal(raw, &reports); err != nil || len(reports) != 5 {
		t.Fatalf("expected five driver reports: length=%d err=%v", len(reports), err)
	}
	r := newRig(t)
	ctx := context.Background()
	read := func() store.NodeAddresses {
		t.Helper()
		code, body, _ := r.c.do("GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/addresses", nil, nil)
		if code != 200 {
			t.Fatalf("address GET %d: %s", code, body)
		}
		return mustJSON[store.NodeAddresses](t, body)
	}
	post := func(report agentproto.Report) {
		t.Helper()
		code, body, _ := r.agent.do("POST", "/api/agent/report", report, nil)
		if code != 200 {
			t.Fatalf("agent report %d: %s", code, body)
		}
	}
	post(agentproto.Report{PublicAddresses: &agentproto.PublicAddresses{SampledAt: time.Now().Unix() - 1}})
	code, body, _ := r.c.do("PUT", "/api/admin/settings/nodes/"+itoa(r.nodeID)+"/addresses", map[string]bool{"auto_ipv4": true, "fallback_auto_ipv4": true}, nil)
	if code != 200 {
		t.Fatalf("opt in %d: %s", code, body)
	}
	initial := read()
	if reports[0].PublicAddresses == nil || reports[1].PublicAddresses == nil || reports[0].PublicAddresses.FallbackIPv4 == nil || reports[1].PublicAddresses.FallbackIPv4 == nil {
		t.Fatal("fixture lacks public-address evidence")
	}
	interval := reports[1].PublicAddresses.SampledAt - reports[0].PublicAddresses.SampledAt
	if interval != 120 {
		t.Fatalf("expected 120-second production fixture interval, got %d", interval)
	}
	base := time.Now().Truncate(time.Second).Add(2 * time.Second)
	shift := base.Unix() - reports[0].PublicAddresses.SampledAt
	for i := 0; i < 2; i++ {
		sample := reports[i].PublicAddresses
		sample.SampledAt += shift
		for _, fallback := range []*agentproto.PublicAddressFallback{sample.FallbackIPv4, sample.FallbackIPv6} {
			if fallback != nil {
				fallback.SampledAt += shift
			}
		}
		target := time.Unix(sample.SampledAt, 0).Add(100 * time.Millisecond)
		if delay := time.Until(target); delay > 0 {
			timer := time.NewTimer(delay)
			<-timer.C
		}
		post(reports[i])
		r.web.NodeAddresses().Tick(ctx, time.Now())
		got := read()
		if got.Observed.IPv4.Address != "" || got.Observed.IPv4.ConfirmedAt != 0 {
			t.Fatal("HTTP report conflated single-source and dual-source evidence")
		}
		if i == 0 {
			if got.EntryIPv4 != initial.EntryIPv4 || got.Observed.FallbackFollowIPv4.ConfirmedAt != 0 || got.Observed.FallbackFollowIPv4.RunID != sample.FallbackIPv4.RunID {
				t.Fatalf("first sample did not remain pending: %+v", got)
			}
			post(reports[i])
			if replayed := read(); replayed.Observed.FallbackFollowIPv4 != got.Observed.FallbackFollowIPv4 {
				t.Fatal("duplicate HTTP report changed the candidate")
			}
		} else if got.EntryIPv4 != sample.FallbackIPv4.Address || got.Observed.FallbackFollowIPv4.ConfirmedAt == 0 {
			t.Fatalf("confirmed wire report did not follow: %+v", got)
		}
	}
}
