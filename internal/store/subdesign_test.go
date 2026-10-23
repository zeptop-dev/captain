package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/db"
)

func subDesignStore(t *testing.T) *Store {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return New(conn)
}

func TestApplySubDesignAtomicAndSelectedFormats(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	old, _ := subdesign.Preset("basic")
	if err := s.SetSetting(ctx, SettingSubDesign, old); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, SettingSubTemplates, SubTemplates{"clash": "old", "surge": "keep", "uri": "other"}); err != nil {
		t.Fatal(err)
	}
	next := old
	next.Final = "REJECT"
	if _, err := s.DB().Exec(`CREATE TRIGGER refuse_templates BEFORE UPDATE ON settings WHEN NEW.key = 'sub_templates' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubDesign(ctx, &next, map[string]string{"clash": "new"}); err == nil {
		t.Fatal("expected transaction failure")
	}
	var d subdesign.Design
	if err := s.GetSetting(ctx, SettingSubDesign, &d); err != nil {
		t.Fatal(err)
	}
	if d.Final != old.Final {
		t.Fatal("failed apply persisted its draft")
	}
	if _, err := s.DB().Exec(`DROP TRIGGER refuse_templates`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubDesign(ctx, &next, map[string]string{"clash": "new"}); err != nil {
		t.Fatal(err)
	}
	var templates SubTemplates
	if err := s.GetSetting(ctx, SettingSubTemplates, &templates); err != nil {
		t.Fatal(err)
	}
	if templates["clash"] != "new" || templates["surge"] != "keep" || templates["uri"] != "other" {
		t.Fatalf("unselected templates changed: %v", templates)
	}
}

func TestSubCustomPresetsConcurrentAndIndependent(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	d, _ := subdesign.Preset("basic")
	if err := s.SetSetting(ctx, SettingSubDesign, &d); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"custom:one", "custom:two"} {
		wg.Go(func() {
			results <- s.SaveSubCustomPreset(ctx, SubDesignPreset{Key: key, Name: "Example", Source: "custom", Design: &d, Supported: true})
		})
	}
	wg.Wait()
	close(results)
	wins, duplicates := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrSubPresetName) {
			duplicates++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || duplicates != 1 {
		t.Fatalf("wins=%d duplicates=%d", wins, duplicates)
	}
	var list []SubDesignPreset
	if err := s.GetSetting(ctx, SettingSubCustomPresets, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("custom saves lost isolation: %+v", list)
	}
	if err := s.DeleteSubCustomPreset(ctx, list[0].Key); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubCustomPreset(ctx, list[0].Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing delete: %v", err)
	}
	var active subdesign.Design
	if err := s.GetSetting(ctx, SettingSubDesign, &active); err != nil {
		t.Fatal(err)
	}
	if active.Final != d.Final {
		t.Fatal("deleting a preset changed the active draft")
	}
}

func TestApplySubDesignConcurrentFormats(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	d, _ := subdesign.Preset("basic")
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, format := range []string{"clash", "surge"} {
		wg.Go(func() { results <- s.ApplySubDesign(ctx, &d, map[string]string{format: format}) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var templates SubTemplates
	if err := s.GetSetting(ctx, SettingSubTemplates, &templates); err != nil {
		t.Fatal(err)
	}
	if templates["clash"] != "clash" || templates["surge"] != "surge" {
		t.Fatalf("concurrent apply lost a format: %v", templates)
	}
}
