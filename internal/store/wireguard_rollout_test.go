package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

// Compare every persisted table, including generated keys, versions, ownership
// triggers, sequences and plan evidence, without printing private row contents.
func wireGuardRolloutSnapshot(t *testing.T, s *Store) map[string][32]byte {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := make(map[string][32]byte, len(tables))
	for _, table := range tables {
		rows, err = s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var records []string
		for rows.Next() {
			values, dest := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, string(raw))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(records)
		out[table] = sha256.Sum256([]byte(strings.Join(records, "\n")))
	}
	return out
}

func wireGuardReservationRig(t *testing.T, protocol string) (*rolloutRig, *PathRoute) {
	t.Helper()
	r, draft := pathRolloutRig(t)
	for _, n := range r.nodes {
		if _, err := r.s.db.Exec(`UPDATE nodes SET version='v0.88.0' WHERE id=?`, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	r.ib.Protocol = map[string]spec.Protocol{"tcp": spec.VLESS, "udp": spec.Hysteria2, "both": spec.Shadowsocks}[protocol]
	if err := r.s.UpdateInbound(context.Background(), r.ib); err != nil {
		t.Fatal(err)
	}
	return r, draft
}

func assertWireGuardRollback(t *testing.T, s *Store, before map[string][32]byte) {
	t.Helper()
	after := wireGuardRolloutSnapshot(t, s)
	if len(before) != len(after) {
		t.Fatal("rejected write changed database tables")
	}
	for table, digest := range after {
		if before[table] != digest {
			t.Errorf("rejected write changed table %s", table)
		}
	}
}

func TestWireGuardTunnelHonorsFuturePathReservations(t *testing.T) {
	for _, protocol := range []string{"tcp", "udp", "both"} {
		for _, status := range []string{"running", "paused"} {
			for _, action := range []string{"create", "edit", "enable"} {
				t.Run(protocol+"/"+status+"/"+action, func(t *testing.T) {
					ctx := context.Background()
					r, draft := wireGuardReservationRig(t, protocol)
					port := draft.Entry.Port
					w := WireGuardWrite{
						Link: WireGuardLink{ID: "reserved-tunnel", TransitID: draft.Entry.NodeID, ExitID: r.nodes[3].ID,
							Endpoint: "192.0.2.10:51820", ListenPort: port, MTU: 1380},
						User: domain.Inbound{NodeID: draft.Entry.NodeID, Tag: "wg-subscriber", Protocol: spec.VLESS,
							Port: 28443, Core: "xray", Enabled: action != "enable"},
						Entry: domain.Entry{Name: "WG", DisplayHost: "example.com", DisplayPort: 28443, Enabled: action != "enable"},
					}
					if action != "create" {
						if action == "edit" {
							w.Link.ListenPort++
						}
						if err := r.s.SaveWireGuardLink(ctx, &w); err != nil {
							t.Fatal(err)
						}
					}
					p, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					if status == "paused" {
						if _, err = r.s.PauseLineRollout(ctx, p.ID, p.Version); err != nil {
							t.Fatal(err)
						}
					}
					// The entry listener is still future intent, absent from both
					// persisted forwarding rules and this node's current delivery.
					if fs := r.wire(t, draft.Entry.NodeID); len(fs) != 0 {
						t.Fatal("entry was already released")
					}
					w.Link.ListenPort, w.Link.MTU = port, 1300
					w.User.Enabled, w.Entry.Enabled = true, true
					w.User.Tag, w.Entry.Name = "wg-edited", "Edited WG"
					input, before := w, wireGuardRolloutSnapshot(t, r.s)
					err = r.s.SaveWireGuardLink(ctx, &w)
					if protocol == "tcp" {
						if err != nil {
							t.Fatal("UDP tunnel must coexist with a reserved TCP listener", err)
						}
						if w.Link.Version != input.Link.Version+1 || w.Link.ListenPort != port {
							t.Fatal("allowed write was not committed")
						}
						return
					}
					if !errors.Is(err, ErrLineRolloutConflict) {
						t.Fatalf("future %s listener was not protected: %v", protocol, err)
					}
					if !reflect.DeepEqual(input, w) {
						t.Fatal("rejected write mutated caller state")
					}
					assertWireGuardRollback(t, r.s, before)
					// Rejection leaves the same version reusable at a free port.
					w.Link.ListenPort = port + 100
					if err = r.s.SaveWireGuardLink(ctx, &w); err != nil {
						t.Fatal("failed transaction prevented a valid retry", err)
					}
					if w.Link.Version != input.Link.Version+1 {
						t.Fatal("failed attempt consumed link version")
					}
				})
			}
		}
	}
}

func TestWireGuardReservedTunnelEnableAndRollback(t *testing.T) {
	ctx := context.Background()
	r, draft := wireGuardReservationRig(t, "both")
	p, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.PauseLineRollout(ctx, p.ID, p.Version); err != nil {
		t.Fatal(err)
	}
	w := WireGuardWrite{
		Link: WireGuardLink{ID: "disabled-tunnel", TransitID: draft.Entry.NodeID, ExitID: r.nodes[3].ID,
			Endpoint: "192.0.2.10:51820", ListenPort: draft.Entry.Port, MTU: 1380},
		User:  domain.Inbound{NodeID: draft.Entry.NodeID, Tag: "wg-disabled", Protocol: spec.VLESS, Port: 28443, Core: "xray"},
		Entry: domain.Entry{Name: "Disabled WG", DisplayHost: "example.com", DisplayPort: 28443},
	}
	if err = r.s.SaveWireGuardLink(ctx, &w); err != nil {
		t.Fatal("disabled tunnel must not reserve a socket", err)
	}
	before := wireGuardRolloutSnapshot(t, r.s)
	ib := w.User
	ib.Enabled = true
	// The generic Store path must also notice the owned tunnel. A missing
	// incoming ownership hint cannot hide its actual persisted identity.
	ib.Settings.WireGuardSlot = 0
	if err = r.s.UpdateInbound(ctx, &ib); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("generic inbound update enabled a reserved tunnel", err)
	}
	assertWireGuardRollback(t, r.s, before)

	// The link row is updated before dependent inbound recompilation. A later
	// user-listener conflict must roll back that new port, MTU and version too.
	w.Link.ListenPort = draft.Entry.Port + 100
	w.Link.MTU = 1300
	w.User.Enabled, w.Entry.Enabled = true, true
	w.User.Port = draft.Entry.Port
	input := w
	if err = r.s.SaveWireGuardLink(ctx, &w); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("subscriber port bypassed the plan reservation", err)
	}
	if !reflect.DeepEqual(w, input) {
		t.Fatal("later rejection mutated caller state")
	}
	assertWireGuardRollback(t, r.s, before)

	w.User.Port = 28443
	if err = r.s.SaveWireGuardLink(ctx, &w); err != nil {
		t.Fatal("moving off the reserved tunnel port must allow enabling", err)
	}
	if w.Link.Version != input.Link.Version+1 {
		t.Fatal("rejected writes consumed a version")
	}
}
