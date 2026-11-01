package store

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestPathDeliveryDowngradeChecksWholePersistedPath(t *testing.T) {
	ctx := context.Background()
	s, nodes, _, draft := pathFixture(t)
	if err := s.SavePathRoute(ctx, draft); err != nil {
		t.Fatal(err)
	}
	entryID, downstreamID := nodes[0].ID, nodes[1].ID
	current := lineChangeWire(t, s, entryID)
	ordinary := spec.Forward{Tag: "ordinary-udp", Protocol: "udp", Port: 24000, Target: "198.51.100.20:24001"}
	current = append(current, ordinary)
	var raw string
	if err := s.db.QueryRow(`SELECT forwards_json FROM nodes WHERE id=?`, downstreamID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ protocol, backend string }{{"tcp", ""}, {"udp", ""}, {"tcp", "nft"}, {"both", "realm"}} {
		t.Run(tc.protocol+"/"+tc.backend, func(t *testing.T) {
			var fs []NodeForward
			if err := json.Unmarshal([]byte(raw), &fs); err != nil {
				t.Fatal(err)
			}
			for i := range fs {
				if fs[i].LineID == draft.ID {
					fs[i].Protocol, fs[i].Backend = tc.protocol, tc.backend
				}
			}
			encoded, _ := json.Marshal(fs)
			if _, err := s.db.Exec(`UPDATE nodes SET forwards_json=? WHERE id=?`, string(encoded), downstreamID); err != nil {
				t.Fatal(err)
			}
			for _, version := range []string{"v0.87.0", "v0.88.0"} {
				if _, err := s.db.Exec(`UPDATE nodes SET version=? WHERE id=?`, version, entryID); err != nil {
					t.Fatal(err)
				}
				got, err := s.ApplyLineRolloutForwards(ctx, entryID, current)
				if err != nil {
					t.Fatal(err)
				}
				want := current
				if version == "v0.87.0" && (tc.protocol != "tcp" || tc.backend != "") {
					want = []spec.Forward{ordinary}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: entry ignored another hop's requirement: got %+v want %+v", version, got, want)
				}
			}
		})
	}
}

func TestPathDeliveryDowngradeChecksActivePlanSnapshots(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	p, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	entryID := draft.Entry.NodeID
	entryReleased := false
	for i := 0; i < 20 && !entryReleased; i++ {
		for _, step := range p.Targets {
			if step.Status != "released" {
				continue
			}
			if step.NodeID == entryID {
				entryReleased = true
				break
			}
			r.ack(t, step, true)
			r.advance(t)
			p = r.read(t, p.ID)
			break
		}
	}
	if !entryReleased {
		t.Fatal("entry never released")
	}
	if _, err := r.s.db.Exec(`UPDATE nodes SET version='v0.87.0' WHERE id=?`, entryID); err != nil {
		t.Fatal(err)
	}
	ordinary := spec.Forward{Tag: "ordinary-udp", Protocol: "udp", Port: 24000, Target: "198.51.100.20:24001"}
	before, err := r.s.ApplyLineRolloutForwards(ctx, entryID, []spec.Forward{ordinary})
	if err != nil || len(before) != 2 {
		t.Fatal("legacy native plan must remain supported", before, err)
	}
	for _, target := range p.Nodes {
		if target.NodeID == entryID || len(target.After) == 0 {
			continue
		}
		target.After[0].Backend = "nft"
		raw, _ := json.Marshal(target.After)
		if _, err := r.s.db.Exec(`UPDATE line_rollout_nodes SET after_json=? WHERE plan_id=? AND node_id=?`, string(raw), p.ID, target.NodeID); err != nil {
			t.Fatal(err)
		}
		break
	}
	after, err := r.s.ApplyLineRolloutForwards(ctx, entryID, []spec.Forward{ordinary})
	if err != nil || !reflect.DeepEqual(after, []spec.Forward{ordinary}) {
		t.Fatal("held plan reintroduced a restricted path", after, err)
	}
}
