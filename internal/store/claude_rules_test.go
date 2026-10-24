package store

import (
	"context"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
)

func TestClaudeRulesPublicationIsAtomicAndScoped(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	d, _ := subdesign.Preset("basic")
	d.Enhancements.Claude = true
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "legacy-body"}); err != nil {
		t.Fatal(err)
	}
	library, applied, err := s.ClaudeRulesState(ctx)
	if err != nil || library.Snapshot.Source != "builtin" || applied.Status != "applied" || applied.Snapshot.Revision != "captain-v1" {
		t.Fatalf("legacy state: %+v %+v %v", library, applied, err)
	}
	next := &subdesign.ClaudeRuleSnapshot{Source: "v2fly", Revision: strings.Repeat("a", 40), Rules: []subdesign.ClaudeRule{{Type: "domain", Value: "example.com"}}}
	if err := s.SetSetting(ctx, SettingClaudeRuleLibrary, ClaudeRuleLibrary{Snapshot: next, SyncedAt: 1}); err != nil {
		t.Fatal(err)
	}
	d.Enhancements.ClaudeRules = next
	if _, err := s.DB().Exec(`CREATE TRIGGER refuse_claude_templates BEFORE UPDATE ON settings WHEN NEW.key='sub_templates' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "next-body"}); err == nil {
		t.Fatal("apply should fail")
	}
	_, applied, err = s.ClaudeRulesState(ctx)
	if err != nil || applied.Snapshot.Revision != "captain-v1" {
		t.Fatal("failed apply advanced metadata", applied, err)
	}
	if _, err := s.DB().Exec(`DROP TRIGGER refuse_claude_templates`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "next-body"}); err != nil {
		t.Fatal(err)
	}
	d.Enhancements.Claude = false
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"surge": "surge-only"}); err != nil {
		t.Fatal(err)
	}
	library, applied, err = s.ClaudeRulesState(ctx)
	if err != nil || library.Snapshot.Revision != next.Revision || applied.Status != "applied" || applied.Snapshot.Revision != next.Revision {
		t.Fatal("other-format apply changed clash metadata", applied, err)
	}
	if err := s.SetSetting(ctx, SettingSubTemplates, SubTemplates{"clash": "manual-edit"}); err != nil {
		t.Fatal(err)
	}
	_, applied, err = s.ClaudeRulesState(ctx)
	if err != nil || applied.Status != "unknown" || applied.Snapshot != nil {
		t.Fatal("text edit misreported provenance", applied, err)
	}
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "disabled"}); err != nil {
		t.Fatal(err)
	}
	_, applied, err = s.ClaudeRulesState(ctx)
	if err != nil || applied.Status != "disabled" || applied.Snapshot != nil {
		t.Fatal("disabled state", applied, err)
	}
}
