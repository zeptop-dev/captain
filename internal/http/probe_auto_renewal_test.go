package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeAutoRenewalReportBeatAndFormAPI(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	expired := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	info := store.NodeProbeInfo{ExpiresAt: expired, Billing: &store.NodeCost{Amount: "10", Currency: "USD", Cycle: "monthly"}, AutoRenewal: &store.NodeAutoRenewal{Enabled: true, AnchorDate: "2000-01-01"}}
	save := func(info store.NodeProbeInfo, expected *string, want int) {
		t.Helper()
		body := map[string]any{"Info": info, "Mode": "sum", "ResetDay": 1}
		if expected != nil {
			body["ExpectedExpiresAt"] = *expected
		}
		if status, b, _ := r.c.do("PUT", path, body, nil); status != want {
			t.Fatalf("save: %d %s", status, b)
		}
	}
	read := func() *store.NodeProbe {
		t.Helper()
		p, err := r.st.NodeProbe(ctx, r.nodeID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	invalid := info
	invalid.ExpiresAt = ""
	save(invalid, nil, 400)
	save(info, nil, 200)
	if read().Info.AutoRenewal.AnchorDate != expired {
		t.Fatal("API accepted client anchor")
	}
	// Collection is off by default: normal agent reports still maintain expiry.
	for _, host := range []any{map[string]any{}, map[string]any{"mem_total": 1024, "valid": map[string]bool{}}} {
		if status, b, _ := r.agent.do("POST", "/api/agent/report", map[string]any{"host": host}, nil); status != 200 {
			t.Fatalf("empty report: %d %s", status, b)
		}
		if read().Info.ExpiresAt != expired {
			t.Fatal("empty/invalid resources renewed")
		}
	}
	at := time.Now()
	host := spec.SystemStatus{MemTotal: 1024, Valid: &spec.MetricValidity{Memory: true}, Resources: &spec.Resources{Epoch: "renewal-http", Sequence: 1, At: at.UnixMilli()}}
	if status, b, _ := r.agent.do("POST", "/api/agent/report", map[string]any{"host": host, "traffic_epoch": "invalid", "traffic_seq": 1}, nil); status != 400 {
		t.Fatalf("malformed receipt: %d %s", status, b)
	}
	if read().Info.ExpiresAt != expired {
		t.Fatal("rejected report renewed")
	}
	if status, b, _ := r.agent.do("POST", "/api/agent/report", map[string]any{"host": host}, nil); status != 200 {
		t.Fatalf("valid report: %d %s", status, b)
	}
	renewed := read().Info.ExpiresAt
	if renewed <= expired {
		t.Fatal("report did not renew with collection off")
	}
	info.Note = "stale form changed note"
	save(info, &expired, 200)
	if p := read(); p.Info.ExpiresAt != renewed || p.Info.Note != info.Note {
		t.Fatal("stale form undid renewal")
	}
	info.ExpiresAt = "2030-01-01"
	save(info, &expired, 409)
	info.ExpiresAt = expired
	save(info, &renewed, 200)
	// A known resource payload remains a replay after a manual date change.
	if status, _, _ := r.agent.do("POST", "/api/agent/report", map[string]any{"host": host}, nil); status != 200 {
		t.Fatal(status)
	}
	if read().Info.ExpiresAt != expired {
		t.Fatal("replayed report renewed")
	}
	if status, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": []string{"info"}}, nil); status != 200 {
		t.Fatalf("settings: %d %s", status, b)
	}
	anon := &client{t: t, srv: r.srv}
	if status, _, _ := anon.do("GET", "/api/probe", nil, nil); status != 200 {
		t.Fatal(status)
	}
	host.Resources.Sequence = 2
	host.Resources.At = at.Add(time.Second).UnixMilli()
	if status, b, _ := r.agent.do("POST", "/api/agent/beat", map[string]any{"host": host}, nil); status != 204 {
		t.Fatalf("beat: %d %s", status, b)
	}
	if read().Info.ExpiresAt != renewed {
		t.Fatal("beat failed to renew")
	}
	status, b, _ := anon.do("GET", "/api/probe", nil, nil)
	if status != 200 {
		t.Fatal(status)
	}
	public := mustJSON[struct {
		Nodes []struct {
			Info store.NodeProbeInfo `json:"info"`
		} `json:"nodes"`
	}](t, b)
	if len(public.Nodes) != 1 || public.Nodes[0].Info.ExpiresAt != renewed {
		t.Fatalf("public cached old expiry: %s", b)
	}
}
