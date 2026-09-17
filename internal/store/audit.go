package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// AuditRule is one panel-wide rule every node enforces (block) or watches
// (log). Match uses the route-rule syntax: domain:, full:, keyword:,
// regexp:, ip:, port:, inbound:, geosite:, geoip:, protocol:.
type AuditRule struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Match     []string  `json:"match"`
	Action    string    `json:"action"` // block | log
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditSettings: settings key "audit".
type AuditSettings struct {
	// AutoBanHits bans a user after this many hits within WindowHours
	// (0 = never); the ban is reported to the admin chat and webhooks.
	AutoBanHits int `json:"auto_ban_hits"`
	WindowHours int `json:"window_hours"`
	// NotifyAdmin sends every hit to the admin Telegram chat (batched by
	// report); off by default, the log is enough for most operators.
	NotifyAdmin bool `json:"notify_admin"`
}

const SettingAudit = "audit"

// Window returns the auto-ban window, 24 h when unset.
func (s AuditSettings) Window() time.Duration {
	if s.WindowHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(s.WindowHours) * time.Hour
}

// AuditHit is one reported rule hit.
type AuditHit struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Email      string    `json:"email,omitempty"`
	NodeID     int64     `json:"node_id"`
	NodeName   string    `json:"node_name,omitempty"`
	InboundID  int64     `json:"inbound_id,omitempty"`
	InboundTag string    `json:"inbound_tag,omitempty"`
	RuleID     int64     `json:"rule_id"`
	RuleName   string    `json:"rule_name,omitempty"`
	At         time.Time `json:"at"`
	ClientIP   string    `json:"client_ip"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Action     string    `json:"action"`
}

// AuditRules lists the rules in order; enabledOnly drops disabled ones.
func (s *Store) AuditRules(ctx context.Context, enabledOnly bool) ([]AuditRule, error) {
	q := `SELECT id, name, match_json, action, enabled, sort, created_at FROM audit_rules`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRule
	for rows.Next() {
		var r AuditRule
		var match string
		var enabled int
		var created int64
		if err := rows.Scan(&r.ID, &r.Name, &match, &r.Action, &enabled, &r.Sort, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(match), &r.Match)
		if r.Match == nil {
			r.Match = []string{}
		}
		r.Enabled = enabled == 1
		r.CreatedAt = unix(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceAuditRules stores the whole list (the editor saves it at once);
// ids of kept rules survive so the hit log keeps pointing at them.
func (s *Store) ReplaceAuditRules(ctx context.Context, rules []AuditRule) ([]AuditRule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	keep := map[int64]bool{}
	ts := now()
	for i := range rules {
		r := &rules[i]
		match, _ := json.Marshal(r.Match)
		enabled := 0
		if r.Enabled {
			enabled = 1
		}
		if r.ID > 0 {
			res, err := tx.ExecContext(ctx, `UPDATE audit_rules SET name = ?, match_json = ?, action = ?, enabled = ?, sort = ? WHERE id = ?`, r.Name, string(match), r.Action, enabled, i, r.ID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				keep[r.ID] = true
				continue
			}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO audit_rules (name, match_json, action, enabled, sort, created_at) VALUES (?, ?, ?, ?, ?, ?)`, r.Name, string(match), r.Action, enabled, i, ts)
		if err != nil {
			return nil, err
		}
		r.ID, _ = res.LastInsertId()
		keep[r.ID] = true
	}
	ids, err := tx.QueryContext(ctx, `SELECT id FROM audit_rules`)
	if err != nil {
		return nil, err
	}
	var gone []int64
	for ids.Next() {
		var id int64
		_ = ids.Scan(&id)
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	ids.Close()
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, `DELETE FROM audit_rules WHERE id = ?`, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.AuditRules(ctx, false)
}

// SpecAuditRules is what the nodes receive.
func (s *Store) SpecAuditRules(ctx context.Context) ([]spec.AuditRule, error) {
	rules, err := s.AuditRules(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]spec.AuditRule, 0, len(rules))
	for _, r := range rules {
		if len(r.Match) == 0 {
			continue
		}
		out = append(out, spec.AuditRule{ID: r.ID, Name: r.Name, Match: r.Match, Action: r.Action})
	}
	return out, nil
}

// AddAuditHits stores a node report's hits in one transaction.
func (s *Store) AddAuditHits(ctx context.Context, rows []AuditHit) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log (user_id, node_id, inbound_id, rule_id, at, client_ip, host, port, action) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.UserID, r.NodeID, r.InboundID, r.RuleID, r.At.Unix(), r.ClientIP, r.Host, r.Port, r.Action); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AuditHits lists hits newest first, for one user (userID > 0) or all.
func (s *Store) AuditHits(ctx context.Context, userID int64, limit int) ([]AuditHit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT a.id, a.user_id, COALESCE(u.email, ''), a.node_id, COALESCE(n.name, ''), a.inbound_id, COALESCE(i.tag, ''), a.rule_id, COALESCE(r.name, ''), a.at, a.client_ip, a.host, a.port, a.action
		FROM audit_log a LEFT JOIN users u ON u.id = a.user_id LEFT JOIN nodes n ON n.id = a.node_id LEFT JOIN inbounds i ON i.id = a.inbound_id LEFT JOIN audit_rules r ON r.id = a.rule_id`
	args := []any{}
	if userID > 0 {
		q += ` WHERE a.user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY a.at DESC, a.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditHit
	for rows.Next() {
		var r AuditHit
		var at int64
		if err := rows.Scan(&r.ID, &r.UserID, &r.Email, &r.NodeID, &r.NodeName, &r.InboundID, &r.InboundTag, &r.RuleID, &r.RuleName, &at, &r.ClientIP, &r.Host, &r.Port, &r.Action); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountUserAuditHits counts a user's hits since the given time.
func (s *Store) CountUserAuditHits(ctx context.Context, userID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND at >= ?`, userID, since.Unix()).Scan(&n)
	return n, err
}

// CountBlockedAuditHits counts only the hits of block rules, which are the
// ones the automatic ban is about: a "log only" rule is for watching.
func (s *Store) CountBlockedAuditHits(ctx context.Context, userID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND at >= ? AND action = 'block'`, userID, since.Unix()).Scan(&n)
	return n, err
}

// RuleHitCounts returns hits per rule since the given time.
func (s *Store) RuleHitCounts(ctx context.Context, since time.Time) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rule_id, COUNT(*) FROM audit_log WHERE at >= ? GROUP BY rule_id`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// PruneAuditLog deletes hits older than before, in batches (see
// PruneConnLog).
func (s *Store) PruneAuditLog(ctx context.Context, before time.Time) (int64, error) {
	const batch = 20000
	var total int64
	for {
		res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log WHERE at < ? LIMIT ?)`, before.Unix(), batch)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batch {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
}
