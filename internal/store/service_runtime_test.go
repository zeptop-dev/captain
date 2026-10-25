package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestNodeRuntimeUsesCoherentAcceptedReport(t *testing.T) {
	s, id := nodeRenewalRig(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	rep := agentproto.Report{Version: "v0.80.0", Revision: "fetched", AppliedRevision: "applied", TrafficEpoch: "journal-one", TrafficSeq: 1, Cores: map[string]agentproto.CoreStatus{"xray": {Running: true, Inbounds: []string{"inbound"}}}, Forwards: []agentproto.ForwardStatus{{Tag: "rule", Health: "up", ProbeProtocol: "tcp"}}, WireGuard: &agentproto.WireGuardReport{Available: true, Applied: true}}
	if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at); err != nil {
		t.Fatal(err)
	}
	original, err := s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if original == nil || original.Sequence != 1 || !original.ObservedAt.Equal(at) || original.Revision != "fetched" || original.AppliedRevision != "applied" || !original.Cores["xray"].Running {
		t.Fatalf("missing coherent runtime: %#v", original)
	}
	// A resource-only heartbeat refreshes ordinary liveness, not runtime.
	if err := s.RecordBeat(ctx, id, spec.SystemStatus{}, at.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Retried/delayed receipts may carry changed runtime metadata. They cannot
	// extend or rewrite the last independent observation.
	rep.Revision = "retry"
	rep.AppliedRevision = ""
	rep.Cores = nil
	rep.Forwards = nil
	rep.WireGuard = nil
	if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", rep, false, at.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	unchanged, err := s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, unchanged) {
		t.Fatal("beat/retry changed runtime")
	}
	// A genuinely new report which omits fields clears them, rather than
	// lending its timestamp to the previous core/forward/WG evidence.
	rep.TrafficSeq = 2
	if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Sequence != 2 || cleared.Cores != nil || cleared.Forwards != nil || cleared.WireGuard != nil || cleared.AppliedRevision != "" {
		t.Fatal("omitted runtime fields inherited prior evidence")
	}
	encoded, err := json.Marshal(cleared)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["Identity"] != nil || fields["identity"] != nil {
		t.Fatal("identity can be serialized")
	}
}

func TestNodeRuntimePairingAndUnnumberedObservations(t *testing.T) {
	s, id := nodeRenewalRig(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	rep := agentproto.Report{Version: "v0.80.0", Revision: "current", Cores: map[string]agentproto.CoreStatus{}}
	for i := 0; i < 2; i++ {
		if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil || runtime.Sequence != 0 {
		t.Fatal("unnumbered retries counted as independent samples")
	}
	if err := s.ResetPairCode(ctx, id, "runtime-new-pair", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, "runtime-new-pair", "new-runtime-token", "test", "v0.80.0", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatal("old node restored runtime", err)
	}
	runtime, err = s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if runtime != nil {
		t.Fatal("new pairing inherited runtime")
	}
	rep.TrafficSeq = 1
	if _, err := s.RecordNodeReport(ctx, id, "new-runtime-token", rep, true, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	runtime, err = s.NodeRuntime(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Sequence != 1 || runtime.Identity != "new-runtime-token" {
		t.Fatal("new pairing did not start independent evidence")
	}
}
