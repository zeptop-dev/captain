package http

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

// Exercise the actual authenticated report path: traffic receipt acceptance,
// full runtime capture and resource-only heartbeat are separate operations.
func TestServiceRuntimeHTTPReportRetriesAndBeats(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(path string, input any, want int) {
		t.Helper()
		if code, body, _ := r.agent.do("POST", path, input, nil); code != want {
			t.Fatalf("%s: %d want %d: %s", path, code, want, body)
		}
	}
	if code, body, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public"}, nil); code != 200 {
		t.Fatalf("enable beats: %d %s", code, body)
	}
	read := func() *store.NodeRuntime {
		t.Helper()
		v, err := r.st.NodeRuntime(ctx, r.nodeID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	caps := spec.CapabilitiesForCore("xray")
	rep := agentproto.Report{Version: "v0.80.0", Revision: "fetched-runtime", AppliedRevision: "applied-runtime", TrafficEpoch: "0123456789abcdef0123456789abcdef", TrafficSeq: 1, Cores: map[string]agentproto.CoreStatus{"xray": {Running: true, Capabilities: &caps, Inbounds: []string{"in"}}}, Forwards: []agentproto.ForwardStatus{{Tag: "runtime-rule", Health: "up", ProbeProtocol: "tcp"}}, WireGuard: &agentproto.WireGuardReport{Available: true, Applied: true, Links: []agentproto.WireGuardLinkStatus{{Slot: 1, Connected: true, Handshake: time.Now().Unix()}}}}
	call("/api/agent/report", rep, 200)
	initial := read()
	if initial == nil || initial.Sequence != 1 || initial.Cores["xray"].Inbounds[0] != "in" || len(initial.Forwards) != 1 || initial.WireGuard == nil || initial.AppliedRevision != "applied-runtime" {
		t.Fatal("accepted report omitted runtime evidence")
	}
	// Age the genuine report clock without waiting. A fast resource heartbeat
	// must not make its proxy evidence fresh again.
	old := time.Now().Add(-4 * time.Minute).Truncate(time.Second)
	if _, err := r.st.DB().Exec(`UPDATE nodes SET runtime_reported_at=? WHERE id=?`, old.Unix(), r.nodeID); err != nil {
		t.Fatal(err)
	}
	aged := read()
	call("/api/agent/beat", agentproto.Beat{Version: "v0.80.0", Host: spec.SystemStatus{MemTotal: 1024}}, 204)
	if !reflect.DeepEqual(aged, read()) {
		t.Fatal("resource beat refreshed or changed proxy runtime")
	}
	// These metadata changes are intentional: the old receipt must not replace
	// the coherent snapshot even when the legacy status columns are updated.
	rep.Revision = "retry-revision"
	rep.AppliedRevision = ""
	rep.Cores = nil
	rep.Forwards = nil
	rep.WireGuard = nil
	for range 2 {
		call("/api/agent/report", rep, 200)
		if !reflect.DeepEqual(aged, read()) {
			t.Fatal("receipt replay rewrote runtime")
		}
	}
	// Validation failures occur after some legacy diagnostic writes, but must
	// never reach the independently accepted runtime snapshot.
	invalid := rep
	invalid.TrafficEpoch = "invalid-epoch"
	invalid.TrafficSeq = 2
	call("/api/agent/report", invalid, 400)
	if !reflect.DeepEqual(aged, read()) {
		t.Fatal("rejected full report changed runtime")
	}
	rep.TrafficSeq = 2
	call("/api/agent/report", rep, 200)
	next := read()
	if next.Sequence != 2 || !next.ObservedAt.After(aged.ObservedAt) || next.Cores != nil || next.Forwards != nil || next.WireGuard != nil || next.AppliedRevision != "" {
		t.Fatal("new complete report borrowed earlier evidence")
	}
	// The pairing token revocation happens before any later request can restore
	// the snapshot. A replacement identity starts with no runtime observations.
	if err := r.st.ResetPairCode(ctx, r.nodeID, "runtime-replacement", time.Minute); err != nil {
		t.Fatal(err)
	}
	call("/api/agent/report", rep, 401)
	if read() != nil {
		t.Fatal("revoked node restored old runtime")
	}
}
