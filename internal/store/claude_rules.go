package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
)

const SettingClaudeRuleLibrary = "sub_claude_rule_library"
const SettingClaudeRuleApplied = "sub_claude_rule_applied"
const SettingClaudeRuleAppliedFormats = "sub_claude_rule_applied_formats"

// ClaudeRuleLibrary is independent of saved designs and published templates.
type ClaudeRuleLibrary struct {
	Snapshot *subdesign.ClaudeRuleSnapshot `json:"snapshot"`
	SyncedAt int64                         `json:"synced_at"`
}

type ClaudeRuleApplied struct {
	Status    string                        `json:"status"`
	Snapshot  *subdesign.ClaudeRuleSnapshot `json:"snapshot,omitempty"`
	AppliedAt int64                         `json:"applied_at,omitempty"`
}

type claudeAppliedRecord struct {
	ClaudeRuleApplied
	TemplateHash string `json:"template_hash"`
}

func templateHash(body string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(body))) }

func writeClaudeAppliedTx(ctx context.Context, tx *sql.Tx, d *subdesign.Design, rendered map[string]string) error {
	records := map[string]claudeAppliedRecord{}
	if err := readSettingTx(ctx, tx, SettingClaudeRuleAppliedFormats, &records); err != nil {
		return err
	}
	if records == nil {
		records = map[string]claudeAppliedRecord{}
	}
	// A publication may intentionally update only some clients. Keep the other
	// clients' provenance tied to their existing templates, not the new draft.
	appliedAt := time.Now().Unix()
	for format, body := range rendered {
		record := claudeAppliedRecord{ClaudeRuleApplied: ClaudeRuleApplied{Status: "disabled", AppliedAt: appliedAt}, TemplateHash: templateHash(body)}
		if d.Enhancements.Claude {
			record.Status = "applied"
			record.Snapshot = d.Enhancements.ClaudeRules
			if record.Snapshot == nil {
				record.Snapshot = subdesign.BuiltinClaudeRules()
			}
		}
		records[format] = record
		if format == "clash" {
			// Keep the original setting valid for older Captain versions.
			if err := writeSettingTx(ctx, tx, SettingClaudeRuleApplied, record); err != nil {
				return err
			}
		}
	}
	return writeSettingTx(ctx, tx, SettingClaudeRuleAppliedFormats, records)
}

// ClaudeRulesState reads the cache and the global Mihomo publication together.
// Older publications or text edits have unknown provenance; never infer it
// from the current draft, which can differ from the published template.
func (s *Store) ClaudeRulesState(ctx context.Context) (ClaudeRuleLibrary, ClaudeRuleApplied, error) {
	library, applied, err := s.ClaudeRulesFormatsState(ctx)
	return library, applied["clash"], err
}

// ClaudeRulesFormatsState reads all global template publications in the same
// snapshot. It does not describe subscription profiles or response overrides.
func (s *Store) ClaudeRulesFormatsState(ctx context.Context) (ClaudeRuleLibrary, map[string]ClaudeRuleApplied, error) {
	library := ClaudeRuleLibrary{Snapshot: subdesign.BuiltinClaudeRules()}
	applied := map[string]ClaudeRuleApplied{}
	for _, format := range subdesign.Formats {
		applied[format] = ClaudeRuleApplied{Status: "unknown"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library, applied, err
	}
	defer tx.Rollback()
	if err = readSettingTx(ctx, tx, SettingClaudeRuleLibrary, &library); err != nil {
		return library, applied, err
	}
	if library.Snapshot == nil {
		library.Snapshot = subdesign.BuiltinClaudeRules()
	}
	var records map[string]claudeAppliedRecord
	if err = readSettingTx(ctx, tx, SettingClaudeRuleAppliedFormats, &records); err != nil {
		return library, applied, err
	}
	if records == nil {
		records = map[string]claudeAppliedRecord{}
	}
	if _, exists := records["clash"]; !exists {
		var legacy claudeAppliedRecord
		if err = readSettingTx(ctx, tx, SettingClaudeRuleApplied, &legacy); err != nil {
			return library, applied, err
		}
		records["clash"] = legacy
	}
	var templates SubTemplates
	if err = readSettingTx(ctx, tx, SettingSubTemplates, &templates); err != nil {
		return library, applied, err
	}
	for format := range applied {
		record := records[format]
		if body, exists := templates[format]; exists && record.TemplateHash == templateHash(body) {
			applied[format] = record.ClaudeRuleApplied
		}
	}
	return library, applied, tx.Commit()
}
