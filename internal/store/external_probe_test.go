package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
)

func TestExternalProbeHidesDead(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	src := &ExternalSource{Name: "air", URL: "https://air.example/sub", Enabled: true, HideDead: true}
	if err := s.SaveExternalSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	_ = s.ReplaceSourceNodes(ctx, src, []ExternalNode{{Name: "ok", URI: "ss://YWVzLTEyOC1nY206cHc@203.0.113.30:443#ok"}, {Name: "dead", URI: "ss://YWVzLTEyOC1nY206cHc@203.0.113.31:443#dead"}}, "", at)
	all, _ := s.ListExternalNodes(ctx)
	if len(all) != 2 {
		t.Fatalf("nodes: %d", len(all))
	}
	for _, n := range all {
		if n.Name == "dead" {
			_ = s.SetExternalProbe(ctx, n.ID, -1, "timeout", at)
		} else {
			_ = s.SetExternalProbe(ctx, n.ID, 12.5, "", at)
		}
	}
	visible, err := s.ExternalNodesForGroup(ctx, nil)
	if err != nil || len(visible) != 1 || visible[0].Name != "ok" || visible[0].ProbeMs != 12.5 {
		t.Fatalf("visible = %+v, %v", visible, err)
	}
	srcs, _ := s.ListExternalSources(ctx)
	if len(srcs) != 1 || !srcs[0].HideDead {
		t.Fatalf("hide_dead lost: %+v", srcs)
	}
	src.HideDead = false
	_ = s.SaveExternalSource(ctx, src)
	if visible, _ = s.ExternalNodesForGroup(ctx, nil); len(visible) != 2 {
		t.Fatalf("hide_dead off should show both: %d", len(visible))
	}
}
