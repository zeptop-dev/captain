package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPathReferencePreventsStagedSourceDeletion(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	path := &ForwardPath{Name: "Pinned source", Steps: []PathStep{{LineID: r.line.ID, LineVersion: r.line.Version}}}
	if err := r.s.SaveForwardPath(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.PreviewLineRollout(ctx, cloneLine(r.line), true, time.Minute); !errors.Is(err, ErrPathInUse) {
		t.Fatalf("referenced delete preview = %v", err)
	}
	if _, err := r.s.PlanLineRollout(ctx, cloneLine(r.line), true, time.Minute); !errors.Is(err, ErrPathInUse) {
		t.Fatalf("referenced deletion plan = %v", err)
	}
	var count int
	if err := r.s.db.QueryRow(`SELECT count(*) FROM line_rollouts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected deletion wrote plan", count, err)
	}
	if got := pathOwned(t, r.s, r.nodes[0].ID, r.line.ID); len(got) != 1 {
		t.Fatal("rejected deletion changed source", got)
	}
	// The final transactional boundary independently protects pinned revisions.
	tx, err := r.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = deleteLineRolloutTx(ctx, tx, &LineRollout{LineID: r.line.ID, Before: r.line, baseVersion: r.line.Version}); !errors.Is(err, ErrPathInUse) {
		t.Fatalf("final deletion bypassed path ownership: %v", err)
	}
}

func TestPathCannotPinUnpublishedOrDeletingRollout(t *testing.T) {
	for _, kind := range []string{"create", "delete", "paused-delete"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			r := newRolloutRig(t)
			remove := kind != "create"
			if remove {
				if err := r.s.SaveLine(ctx, r.line); err != nil {
					t.Fatal(err)
				}
			}
			p, err := r.s.PlanLineRollout(ctx, cloneLine(r.line), remove, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "paused-delete" {
				p, err = r.s.PauseLineRollout(ctx, p.ID, p.Version)
				if err != nil {
					t.Fatal(err)
				}
			}
			line := mustRolloutLine(t, r.s, p.LineID)
			path := &ForwardPath{Name: "Unavailable source", Steps: []PathStep{{LineID: line.ID, LineVersion: line.Version}}}
			if err = r.s.SaveForwardPath(ctx, path); !errors.Is(err, ErrLineRolloutConflict) {
				t.Fatalf("pin during %s = %v", kind, err)
			}
			for _, table := range []string{"forward_paths", "forward_path_line_versions", "forward_path_refs"} {
				var count int
				if err = r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("failed pin left data", table, count, err)
				}
			}
			if kind == "create" {
				r.drain(t, p.ID)
				if err = r.s.SaveForwardPath(ctx, path); err != nil {
					t.Fatal("published creation still cannot be pinned", err)
				}
			}
		})
	}
}

func TestPathCanPinPublishedSourceDuringStagedEdit(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	path := &ForwardPath{Name: "Published source", Steps: []PathStep{{LineID: r.line.ID, LineVersion: r.line.Version}}}
	if err = r.s.SaveForwardPath(ctx, path); err != nil {
		t.Fatal("published source was unnecessarily blocked", err)
	}
	r.drain(t, p.ID)
	path.Name = "Still pinned"
	if err = r.s.SaveForwardPath(ctx, path); err != nil {
		t.Fatal("published pin changed after rollout", err)
	}
}
