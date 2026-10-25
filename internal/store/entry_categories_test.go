package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
)

func TestEntryCategoriesLifecycleAndAtomicAssignment(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	a := &domain.EntryCategory{Name: "  优选线路  ", Description: "  平台展示分类  "}
	b := &domain.EntryCategory{Name: "Business"}
	for _, c := range []*domain.EntryCategory{a, b} {
		if err := s.CreateEntryCategory(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if a.Name != "优选线路" || a.Description != "平台展示分类" || a.Sort >= b.Sort {
		t.Fatalf("normalization/order: %+v %+v", a, b)
	}
	if err := s.CreateEntryCategory(ctx, &domain.EntryCategory{Name: " business "}); !errors.Is(err, ErrEntryCategoryConflict) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	for _, bad := range []domain.EntryCategory{{Name: " "}, {Name: "bad\nname"}, {Name: "<script>"}, {Name: strings.Repeat("类", 81)}, {Name: "valid", Description: strings.Repeat("文", 501)}} {
		if err := s.CreateEntryCategory(ctx, &bad); !errors.Is(err, ErrEntryCategoryInvalid) {
			t.Fatalf("invalid category accepted: %v", err)
		}
	}
	e1 := &domain.Entry{Name: "First", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Rate: 2, Sort: 10, Tags: []string{"custom"}, Region: "JP", Enabled: true}
	e2 := &domain.Entry{Name: "Second", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Rate: 3, Sort: 20, Enabled: true, CategoryID: &b.ID}
	for _, e := range []*domain.Entry{e1, e2} {
		if err := s.CreateEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	u := &domain.User{Email: "customer@example.com", UUID: "category-test", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{{e1.ID, 99999}, {e1.ID, e1.ID}, {}, {-1}} {
		if err := s.AssignEntryCategory(ctx, ids, a.ID); !errors.Is(err, ErrEntryCategoryInvalid) {
			t.Fatal("invalid batch accepted", ids, err)
		}
		got, err := s.EntryByID(ctx, e1.ID)
		if err != nil || got.CategoryID != nil {
			t.Fatal("partial assignment survived rollback", got, err)
		}
	}
	for _, invalid := range []int64{-1, 99999} {
		if err := s.AssignEntryCategory(ctx, []int64{e1.ID}, invalid); !errors.Is(err, ErrEntryCategoryInvalid) {
			t.Fatal("invalid category accepted", invalid, err)
		}
	}
	if err := s.AssignEntryCategory(ctx, []int64{e1.ID, e2.ID}, a.ID); err != nil {
		t.Fatal(err)
	}
	// The separate user-facing query has its own entry scanner. Category
	// metadata must survive it, while the publication order and tags stay put.
	visible, err := s.EntriesForUser(ctx, u)
	if err != nil || len(visible) != 2 || visible[0].Entry.ID != e1.ID || visible[1].Entry.ID != e2.ID {
		t.Fatal("publication order changed", visible, err)
	}
	for _, row := range visible {
		if row.Entry.CategoryID == nil || *row.Entry.CategoryID != a.ID {
			t.Fatal("category lost by user scanner", row)
		}
	}
	if !reflect.DeepEqual(visible[0].Entry.Tags, e1.Tags) || visible[0].Entry.Rate != 2 || visible[0].Entry.Region != "JP" {
		t.Fatal("assignment altered routing/presentation metadata", visible[0])
	}
	// Complete-list sorting rejects stale, duplicate and unknown identifiers.
	for _, ids := range [][]int64{{b.ID}, {b.ID, b.ID}, {b.ID, 99999}} {
		if err := s.ReorderEntryCategories(ctx, ids); err == nil {
			t.Fatal("invalid reorder accepted", ids)
		}
		got, err := s.ListEntryCategories(ctx)
		if err != nil || got[0].ID != a.ID || got[1].ID != b.ID {
			t.Fatal("partial reorder survived rollback", got, err)
		}
	}
	if err := s.ReorderEntryCategories(ctx, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	b.Name, b.Description, b.Sort = "Business renamed", "Plain description", 999
	if err := s.UpdateEntryCategory(ctx, b); err != nil || b.Sort != 10 {
		t.Fatal("editor overwrote category ordering", b, err)
	}
	if err := s.AssignEntryCategory(ctx, []int64{e2.ID}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEntryCategory(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListEntries(ctx)
	if err != nil || len(got) != 2 || got[0].CategoryID != nil || got[1].CategoryID != nil || got[0].ID != e1.ID || got[0].Sort != 10 {
		t.Fatal("delete did not preserve unclassified entries", got, err)
	}
	if err := s.DeleteEntryCategory(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing category deletion", err)
	}
}

func TestEntryCategoryMigrationPreservesLegacyEntries(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, conn, ".", 86); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'Legacy',0,0)`,
		`INSERT INTO inbounds(id,node_id,tag,protocol,port,created_at,updated_at) VALUES(1,1,'legacy','vless',443,0,0)`,
		`INSERT INTO entries(id,name,inbound_id,display_host,display_port,sort,created_at,updated_at) VALUES(1,'Legacy',1,'example.com',443,70,0,0)`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	e, err := s.EntryByID(ctx, 1)
	if err != nil || e.CategoryID != nil || e.Sort != 70 || e.DisplayHost != "example.com" {
		t.Fatal("migration changed legacy entry", e, err)
	}
	cs, err := s.ListEntryCategories(ctx)
	if err != nil || cs == nil || len(cs) != 0 {
		t.Fatal("migration invented categories", cs, err)
	}
}

func TestEntryCategorySurvivesLineRecompile(t *testing.T) {
	ctx := context.Background()
	s, _, _, line := lineFixture(t)
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	c := &domain.EntryCategory{Name: "Transit"}
	if err := s.CreateEntryCategory(ctx, c); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "Via transit", LineID: &line.ID, Enabled: true, CategoryID: &c.ID}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	line.Groups[0].Members[0].Port++
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	got, err := s.EntryByID(ctx, e.ID)
	if err != nil || got.CategoryID == nil || *got.CategoryID != c.ID || got.DisplayPort != line.Groups[0].Members[0].Port {
		t.Fatal("line recompilation lost category", got, err)
	}
}

func TestEntryCategorySurvivesReverseConnectionEdits(t *testing.T) {
	for _, kind := range []string{"wireguard", "vless"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, ns, _, _ := lineFixture(t)
			a, b := ns[0], ns[1]
			c := &domain.EntryCategory{Name: "Residential"}
			if err := s.CreateEntryCategory(ctx, c); err != nil {
				t.Fatal(err)
			}
			ib := domain.Inbound{NodeID: a.ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true}
			e := domain.Entry{Name: "Reverse node", DisplayHost: "example.com", DisplayPort: 18443, Enabled: true}
			var id int64
			var save func() error
			if kind == "wireguard" {
				w := WireGuardWrite{Link: WireGuardLink{ID: "wg", TransitID: a.ID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380}, User: ib, Entry: e}
				if err := s.SaveWireGuardLink(ctx, &w); err != nil {
					t.Fatal(err)
				}
				id = w.Entry.ID
				w.Entry.Name = "Updated reverse node"
				save = func() error { return s.SaveWireGuardLink(ctx, &w) }
			} else {
				receiver := ib
				receiver.Tag, receiver.Port = "reverse-receiver", 19443
				writes := []ReverseWrite{{Link: ReverseLink{ID: "reverse", ExitID: b.ID, TransitID: a.ID}, User: ib, Receiver: receiver, Entry: e}}
				if err := s.SaveReverseLinks(ctx, b.ID, nil, writes); err != nil {
					t.Fatal(err)
				}
				id = writes[0].Entry.ID
				writes[0].Link.Version = 1
				writes[0].Entry.Name = "Updated reverse node"
				save = func() error { return s.SaveReverseLinks(ctx, b.ID, map[string]int64{"reverse": 1}, writes) }
			}
			got, err := s.EntryByID(ctx, id)
			if err != nil || got.CategoryID != nil {
				t.Fatal("new managed entry should be unclassified", got, err)
			}
			if err := s.AssignEntryCategory(ctx, []int64{id}, c.ID); err != nil {
				t.Fatal(err)
			}
			if err := save(); err != nil {
				t.Fatal(err)
			}
			got, err = s.EntryByID(ctx, id)
			if err != nil || got.CategoryID == nil || *got.CategoryID != c.ID || got.Name != "Updated reverse node" {
				t.Fatal("partial reverse editor lost assigned category", got, err)
			}
		})
	}
}

func TestEntryCategoryTokenScope(t *testing.T) {
	if err := ValidateTokenScopes([]string{"entry-categories:read", "entry-categories:write"}); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{`full|["entries:*"]`, `full|["entry-categories:read"]`, `read|["entry-categories:*"]`} {
		if TokenAllowsRequest(policy, "DELETE", "/api/admin/entry-categories/1", "DELETE /api/admin/entry-categories/{id}") {
			t.Fatal("narrow/read scope allowed category deletion", policy)
		}
	}
	if !TokenAllowsRequest(`full|["entry-categories:write"]`, "DELETE", "/api/admin/entry-categories/1", "DELETE /api/admin/entry-categories/{id}") {
		t.Fatal("explicit write scope denied")
	}
}
