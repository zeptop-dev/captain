package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestLineRolloutUnionTransportReservations(t *testing.T) {
	for _, tc := range []struct {
		old, next string
		conflict  bool
	}{
		{"tcp", "udp", false}, {"udp", "tcp", false},
		{"tcp", "tcp", true}, {"udp", "udp", true},
		{"both", "tcp", true}, {"both", "udp", true},
		{"tcp", "both", true}, {"udp", "both", true}, {"both", "both", true},
	} {
		t.Run(tc.old+"-"+tc.next, func(t *testing.T) {
			old := LineChangeForward{Tag: "old", Listen: "0.0.0.0", Port: 23456, Protocol: tc.old, Backend: "relay"}
			next := LineChangeForward{Tag: "next", Listen: "192.0.2.10", Port: 23456, Protocol: tc.next, Backend: "nft"}
			union, err := lineRolloutUnion([]LineChangeForward{old}, []LineChangeForward{next})
			if errors.Is(err, ErrLineRolloutUnsafe) != tc.conflict || (err != nil && !tc.conflict) {
				t.Fatalf("conflict=%v, got %v", tc.conflict, err)
			}
			if !tc.conflict && len(union) != 2 {
				t.Fatal("lost independent transport", union)
			}
			// Disjoint concrete addresses do not reserve each other's socket.
			old.Listen, next.Listen = "192.0.2.20", "192.0.2.10"
			if _, err = lineRolloutUnion([]LineChangeForward{old}, []LineChangeForward{next}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRolloutGuardsReserveEachTransport(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	nodeID := r.nodes[0].ID
	for _, reserved := range []string{"tcp", "udp", "both"} {
		t.Run(reserved, func(t *testing.T) {
			// Isolate the guard from compiler validation: persisted reservations
			// are authoritative even before that listener has been released.
			raw, _ := json.Marshal([]LineChangeForward{{Tag: "future", Listen: "0.0.0.0", Port: 23456, Protocol: reserved, Backend: "nft"}})
			if _, err := r.s.db.Exec(`UPDATE line_rollout_nodes SET before_json='[]',after_json=? WHERE plan_id=? AND node_id=?`, string(raw), p.ID, nodeID); err != nil {
				t.Fatal(err)
			}
			tx, err := r.s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for _, tc := range []struct {
				protocol spec.Protocol
				network  string
			}{
				{spec.VLESS, "tcp"}, {spec.Hysteria2, "udp"}, {spec.Shadowsocks, "both"},
			} {
				wantConflict := reserved == "both" || tc.network == "both" || reserved == tc.network
				ib := &domain.Inbound{NodeID: nodeID, Tag: "new", Protocol: tc.protocol, Port: 23456, Listen: "192.0.2.10", Enabled: true}
				err = checkRolloutInboundTx(ctx, tx, ib)
				if errors.Is(err, ErrLineRolloutConflict) != wantConflict || (err != nil && !wantConflict) {
					t.Fatalf("inbound %s vs %s: %v", tc.network, reserved, err)
				}
				f := NodeForward{Forward: spec.Forward{Tag: "new", Protocol: tc.network, Port: 23456, Listen: "192.0.2.10", Target: "198.51.100.20:443"}}
				err = checkRolloutForwardsTx(ctx, tx, nodeID, []NodeForward{f})
				if errors.Is(err, ErrLineRolloutConflict) != wantConflict || (err != nil && !wantConflict) {
					t.Fatalf("forward %s vs %s: %v", tc.network, reserved, err)
				}
			}
		})
	}
}

func TestPathRolloutSnapshotVersionsStayInTheirPermissionScope(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	p, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.SnapshotVersion != "path-forward-v2" {
		t.Fatalf("new plan has wrong snapshot: %q", p.SnapshotVersion)
	}
	for _, version := range []string{"path-relay-v1", "path-forward-v2"} {
		t.Run(version, func(t *testing.T) {
			if _, err := r.s.db.Exec(`UPDATE line_rollouts SET snapshot_version=? WHERE id=?`, version, p.ID); err != nil {
				t.Fatal(err)
			}
			paths, err := r.s.ListPathRollouts(ctx, 0, 0, 20)
			if err != nil || len(paths.Items) != 1 || paths.Items[0].ID != p.ID {
				t.Fatal(paths, err)
			}
			lines, err := r.s.ListLineRollouts(ctx, 0, 0, 20)
			if err != nil || len(lines.Items) != 0 {
				t.Fatal(lines, err)
			}
			access := allOperationAccess()
			access.PathRollouts = false
			if got := operationList(t, r.s, OperationQuery{}, access, r.at); len(got.Items) != 0 {
				t.Fatal("ordinary scope exposed path history", got)
			}
			if _, err = r.s.Operation(ctx, "line_rollout:"+p.ID, allOperationAccess(), r.at); !errors.Is(err, ErrNotFound) {
				t.Fatal("spoofed ordinary source", err)
			}
			got := operationList(t, r.s, OperationQuery{Source: "path_rollout"}, allOperationAccess(), r.at)
			if len(got.Items) != 1 || got.Items[0].Source != "path_rollout" {
				t.Fatal(got)
			}
			if _, err = r.s.Operation(ctx, "path_rollout:"+p.ID, allOperationAccess(), r.at); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTopologyLinePreviewHonorsUDPAndBothReservations(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(protocol string) {
		t.Helper()
		raw, _ := json.Marshal([]LineChangeForward{{Tag: "future", Port: 23456, Protocol: protocol, Backend: "nft"}})
		if _, err := r.s.db.Exec(`UPDATE line_rollout_nodes SET before_json='[]',after_json=? WHERE plan_id=? AND node_id=?`, string(raw), p.ID, r.nodes[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	reserve("udp")
	line := &ForwardLine{Name: "Independent TCP", Protocol: "tcp", Backend: "relay", TargetInboundID: r.ib.ID,
		Groups: []LineGroup{{Members: []LineMember{{NodeID: r.nodes[0].ID, Port: 23456}}}}}
	d := &TopologyChangeset{Name: "TCP beside reserved UDP", Operations: []TopologyOperation{{Key: "line", Kind: "line", Line: line}}}
	if err := r.s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	preview, err := r.s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal("UDP-only reservation incorrectly blocked an independent TCP line", err)
	}
	reserve("both")
	if _, err := r.s.PreviewTopologyChangeset(ctx, d.ID, d.Version); err == nil {
		t.Fatal("both reservation was bypassed by topology preview")
	}
	if _, err := r.s.CommitTopologyChangeset(ctx, d.ID, d.Version, preview.Revision); err == nil {
		t.Fatal("commit ignored the changed transport reservation")
	}
}

func TestPathRolloutBackendOmissionAndLegacyCompensation(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	if _, err := r.s.db.Exec(`UPDATE nodes SET version='v0.88.0'`); err != nil {
		t.Fatal(err)
	}
	draft.Branches = draft.Branches[:1]
	p, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	p = r.drain(t, p.ID)
	route, err := r.s.PathRoute(ctx, p.LineID)
	if err != nil {
		t.Fatal(err)
	}
	route.EntryBackend = "realm"
	route.Entry.Port += 1000
	for i := range route.Branches[0].Ports {
		route.Branches[0].Ports[i] += 1000
	}
	edit, err := r.s.PlanPathRollout(ctx, route, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	edit = r.drain(t, edit.ID)
	current, err := r.s.PathRoute(ctx, edit.LineID)
	if err != nil {
		t.Fatal(err)
	}
	current.EntryBackend = "" // omitted/null from an older editor
	preview, err := r.s.PreviewPathRollout(ctx, current, false, time.Minute)
	if err != nil || preview.Draft.EntryBackend != "realm" {
		t.Fatal("staged omission silently reset the entry backend", preview, err)
	}
	// Legacy history had no backend field. Compensation must restore native
	// relay, independently of today's realm choice and omission merge rules.
	if _, err := r.s.db.Exec(`UPDATE path_rollout_intents SET before_json=json_remove(before_json,'$.entry_backend') WHERE plan_id=?`, edit.ID); err != nil {
		t.Fatal(err)
	}
	undo, err := r.s.CompensateLineRollout(ctx, edit.ID, edit.Version)
	if err != nil {
		t.Fatal(err)
	}
	r.drain(t, undo.ID)
	got, err := r.s.PathRoute(ctx, edit.LineID)
	if err != nil || pathWireBackend(got.EntryBackend) != "" {
		t.Fatal("legacy compensation inherited the new backend", got, err)
	}
	for _, f := range pathOwned(t, r.s, draft.Entry.NodeID, edit.LineID) {
		if f.Backend != "" {
			t.Fatal("compensation did not deliver native relay", f)
		}
	}
}
