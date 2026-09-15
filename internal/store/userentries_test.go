package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestUserEntryBlocks(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	n := &domain.Node{Name: "jp"}
	if err := s.CreateNode(ctx, n, "CODE", time.Hour); err != nil {
		t.Fatal(err)
	}
	ib := &domain.Inbound{NodeID: n.ID, Tag: "v", Protocol: spec.VLESS, Port: 443, Enabled: true}
	if err := s.CreateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, name := range []string{"a", "b"} {
		e := &domain.Entry{Name: name, InboundID: ib.ID, DisplayHost: "203.0.113.30", DisplayPort: 443, Enabled: true}
		if err := s.CreateEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	u := &domain.User{Email: "a@b.c", UUID: "u1", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserEntryBlocks(ctx, u.ID, []int64{ids[1]}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EntriesForUser(ctx, u)
	if err != nil || len(got) != 1 || got[0].Entry.Name != "a" {
		t.Fatalf("EntriesForUser = %+v, %v", got, err)
	}
	all, _ := s.EntriesForUserAll(ctx, u)
	if len(all) != 2 {
		t.Fatalf("EntriesForUserAll = %d rows", len(all))
	}
	blocked, _ := s.UserEntryBlocks(ctx, u.ID)
	if len(blocked) != 1 || blocked[0] != ids[1] {
		t.Fatalf("blocks = %v", blocked)
	}
	if err := s.SetUserEntryBlocks(ctx, u.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.EntriesForUser(ctx, u)
	if len(got) != 2 {
		t.Fatalf("after clearing: %d rows", len(got))
	}
}

func TestEntryClientExtraRoundTrip(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	n := &domain.Node{Name: "jp"}
	_ = s.CreateNode(ctx, n, "CODE", time.Hour)
	ib := &domain.Inbound{NodeID: n.ID, Tag: "v", Protocol: spec.VLESS, Port: 443, Enabled: true}
	_ = s.CreateInbound(ctx, ib)
	e := &domain.Entry{Name: "a", InboundID: ib.ID, DisplayHost: "203.0.113.30", DisplayPort: 443, Enabled: true, ClientExtra: map[string]any{"tfo": true, "smux": map[string]any{"enabled": true}}}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	u := &domain.User{Email: "x@y.z", UUID: "u", Status: "active"}
	_ = s.CreateUser(ctx, u)
	rows, err := s.EntriesForUser(ctx, u)
	if err != nil || len(rows) != 1 || rows[0].Entry.ClientExtra["tfo"] != true {
		t.Fatalf("extra lost: %+v %v", rows, err)
	}
	e.ClientExtra = nil
	_ = s.UpdateEntry(ctx, e)
	got, _ := s.ListEntries(ctx)
	if got[0].ClientExtra != nil {
		t.Fatalf("extra not cleared: %v", got[0].ClientExtra)
	}
}
