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

func writeClaudeAppliedTx(ctx context.Context, tx *sql.Tx, d *subdesign.Design, body string) error {
	record := claudeAppliedRecord{ClaudeRuleApplied: ClaudeRuleApplied{Status: "disabled", AppliedAt: time.Now().Unix()}, TemplateHash: templateHash(body)}
	if d.Enhancements.Claude {
		record.Status = "applied"
		record.Snapshot = d.Enhancements.ClaudeRules
		if record.Snapshot == nil {
			record.Snapshot = subdesign.BuiltinClaudeRules()
		}
	}
	return writeSettingTx(ctx, tx, SettingClaudeRuleApplied, record)
}

// ClaudeRulesState reads the cache and the global Mihomo publication together.
// Older publications or text edits have unknown provenance; never infer it
// from the current draft, which can differ from the published template.
func (s *Store) ClaudeRulesState(ctx context.Context) (ClaudeRuleLibrary, ClaudeRuleApplied, error) {
	library := ClaudeRuleLibrary{Snapshot: subdesign.BuiltinClaudeRules()}
	applied := ClaudeRuleApplied{Status: "unknown"}
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
	var record claudeAppliedRecord
	if err = readSettingTx(ctx, tx, SettingClaudeRuleApplied, &record); err != nil {
		return library, applied, err
	}
	var templates SubTemplates
	if err = readSettingTx(ctx, tx, SettingSubTemplates, &templates); err != nil {
		return library, applied, err
	}
	if body, exists := templates["clash"]; exists && record.TemplateHash == templateHash(body) {
		applied = record.ClaudeRuleApplied
	}
	return library, applied, tx.Commit()
}
