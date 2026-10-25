package store

import (
	"context"
	"reflect"
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

func TestClaudeRulesPerFormatPublicationAndLegacyFallback(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	d, _ := subdesign.Preset("basic")
	d.Enhancements.Claude = true
	legacy := claudeAppliedRecord{ClaudeRuleApplied: ClaudeRuleApplied{Status: "applied", Snapshot: subdesign.BuiltinClaudeRules(), AppliedAt: 1}, TemplateHash: templateHash("legacy-clash")}
	if err := s.SetSetting(ctx, SettingClaudeRuleApplied, legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, SettingSubTemplates, SubTemplates{"clash": "legacy-clash", "stash": "untracked-stash"}); err != nil {
		t.Fatal(err)
	}
	_, states, err := s.ClaudeRulesFormatsState(ctx)
	if err != nil || len(states) != len(subdesign.Formats) || !reflect.DeepEqual(states["clash"], legacy.ClaudeRuleApplied) || states["stash"].Status != "unknown" {
		t.Fatal("legacy fallback", states, err)
	}

	next := &subdesign.ClaudeRuleSnapshot{Source: "v2fly", Revision: strings.Repeat("b", 40), Rules: []subdesign.ClaudeRule{{Type: "full", Value: "exact.example.com"}}}
	d.Enhancements.ClaudeRules = next
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"stash": "stash-new", "surge": "surge-new"}); err != nil {
		t.Fatal(err)
	}
	_, before, err := s.ClaudeRulesFormatsState(ctx)
	if err != nil || !reflect.DeepEqual(before["clash"], legacy.ClaudeRuleApplied) {
		t.Fatal("other clients lost legacy clash", before, err)
	}
	for _, format := range []string{"stash", "surge"} {
		if before[format].Status != "applied" || before[format].Snapshot.Revision != next.Revision {
			t.Fatal("wrong per-client version", format, before[format])
		}
	}
	if _, err := s.DB().Exec(`CREATE TRIGGER refuse_multi_claude BEFORE UPDATE ON settings WHEN NEW.key='sub_templates' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	d.Enhancements.Claude = false
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "disabled-clash", "stash": "disabled-stash", "egern": "disabled-egern"}); err == nil {
		t.Fatal("publication should fail atomically")
	}
	_, after, err := s.ClaudeRulesFormatsState(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed transaction changed publication metadata", after, err)
	}
	var oldRecord claudeAppliedRecord
	if err := s.GetSetting(ctx, SettingClaudeRuleApplied, &oldRecord); err != nil || !reflect.DeepEqual(oldRecord, legacy) {
		t.Fatal("failed transaction changed legacy metadata", oldRecord, err)
	}
	if _, err := s.DB().Exec(`DROP TRIGGER refuse_multi_claude`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubDesign(ctx, &d, map[string]string{"clash": "disabled-clash", "stash": "disabled-stash"}); err != nil {
		t.Fatal(err)
	}
	_, after, err = s.ClaudeRulesFormatsState(ctx)
	if err != nil || after["clash"].Status != "disabled" || after["stash"].Status != "disabled" || !reflect.DeepEqual(before["surge"], after["surge"]) || after["egern"].Status != "unknown" {
		t.Fatal("partial publication changed unselected clients", after, err)
	}
	if err := s.SetSetting(ctx, SettingSubTemplates, SubTemplates{"clash": "disabled-clash", "stash": "edited-stash"}); err != nil {
		t.Fatal(err)
	}
	_, after, err = s.ClaudeRulesFormatsState(ctx)
	if err != nil || after["clash"].Status != "disabled" || after["stash"].Status != "unknown" || after["surge"].Status != "unknown" || after["stash"].Snapshot != nil {
		t.Fatal("edited or removed template retained provenance", after, err)
	}
}
