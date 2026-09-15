package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// ExternalSource is an airport subscription (or any share-link list) that
// is re-fetched on a schedule and whose nodes are offered to users.
type ExternalSource struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	UserAgent  string     `json:"user_agent"`
	GroupID    *int64     `json:"group_id"`
	Rate       float64    `json:"rate"`
	Enabled    bool       `json:"enabled"`
	LastSyncAt *time.Time `json:"last_sync_at"`
	LastError  string     `json:"last_error"`
	Nodes      int        `json:"nodes"`
}

// ExternalNode is one share link offered in subscriptions. Traffic through
// it is not accounted (no agent), so it counts nothing against quotas.
type ExternalNode struct {
	ID       int64   `json:"id"`
	SourceID *int64  `json:"source_id"`
	Name     string  `json:"name"`
	URI      string  `json:"uri"`
	GroupID  *int64  `json:"group_id"`
	Rate     float64 `json:"rate"`
	Sort     int     `json:"sort"`
	Enabled  bool    `json:"enabled"`
}

func (s *Store) ListExternalSources(ctx context.Context) ([]ExternalSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.name, s.url, s.user_agent, s.group_id, s.rate, s.enabled, s.last_sync_at, s.last_error,
		(SELECT COUNT(*) FROM external_nodes n WHERE n.source_id = s.id) FROM external_sources s ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalSource{}
	for rows.Next() {
		var e ExternalSource
		var group, last sql.NullInt64
		var en int
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.UserAgent, &group, &e.Rate, &en, &last, &e.LastError, &e.Nodes); err != nil {
			return nil, err
		}
		e.GroupID, e.Enabled, e.LastSyncAt = int64Ptr(group), en == 1, unixPtr(last)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SaveExternalSource(ctx context.Context, e *ExternalSource) error {
	if e.Rate <= 0 {
		e.Rate = 1
	}
	if e.UserAgent == "" {
		e.UserAgent = "v2rayN/7.0"
	}
	if e.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO external_sources (name, url, user_agent, group_id, rate, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.Name, e.URL, e.UserAgent, nullInt64(e.GroupID), e.Rate, boolInt(e.Enabled), now())
		if err != nil {
			return err
		}
		e.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE external_sources SET name = ?, url = ?, user_agent = ?, group_id = ?, rate = ?, enabled = ? WHERE id = ?`,
		e.Name, e.URL, e.UserAgent, nullInt64(e.GroupID), e.Rate, boolInt(e.Enabled), e.ID)
	return err
}

func (s *Store) DeleteExternalSource(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM external_sources WHERE id = ?`, id)
	return err
}

// ReplaceSourceNodes swaps a source's node list after a sync, keeping the
// per-node enabled flag for names that still exist.
func (s *Store) ReplaceSourceNodes(ctx context.Context, src *ExternalSource, nodes []ExternalNode, syncErr string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr == "" {
		disabled := map[string]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT name FROM external_nodes WHERE source_id = ? AND enabled = 0`, src.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			disabled[n] = true
		}
		rows.Close()
		if _, err := tx.ExecContext(ctx, `DELETE FROM external_nodes WHERE source_id = ?`, src.ID); err != nil {
			return err
		}
		for i, n := range nodes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO external_nodes (source_id, name, uri, group_id, rate, sort, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				src.ID, n.Name, n.URI, nullInt64(src.GroupID), src.Rate, i, boolInt(!disabled[n.Name]), at.Unix(), at.Unix()); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE external_sources SET last_sync_at = ?, last_error = ? WHERE id = ?`, at.Unix(), syncErr, src.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListExternalNodes(ctx context.Context) ([]ExternalNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, name, uri, group_id, rate, sort, enabled FROM external_nodes ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalNode{}
	for rows.Next() {
		n, err := scanExternal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func scanExternal(row interface{ Scan(...any) error }) (*ExternalNode, error) {
	var n ExternalNode
	var src, group sql.NullInt64
	var en int
	if err := row.Scan(&n.ID, &src, &n.Name, &n.URI, &group, &n.Rate, &n.Sort, &en); err != nil {
		return nil, wrapNotFound(err)
	}
	n.SourceID, n.GroupID, n.Enabled = int64Ptr(src), int64Ptr(group), en == 1
	return &n, nil
}

// ExternalNodesForGroup returns enabled nodes visible to a user group
// (NULL group = everyone).
func (s *Store) ExternalNodesForGroup(ctx context.Context, groups []int64) ([]ExternalNode, error) {
	clause, args := groupClause("group_id", groups)
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, name, uri, group_id, rate, sort, enabled FROM external_nodes WHERE enabled = 1 AND `+clause+` ORDER BY sort, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalNode{}
	for rows.Next() {
		n, err := scanExternal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *Store) SaveExternalNode(ctx context.Context, n *ExternalNode) error {
	if n.Rate <= 0 {
		n.Rate = 1
	}
	if n.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO external_nodes (source_id, name, uri, group_id, rate, sort, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nullInt64(n.SourceID), n.Name, n.URI, nullInt64(n.GroupID), n.Rate, n.Sort, boolInt(n.Enabled), now(), now())
		if err != nil {
			return err
		}
		n.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE external_nodes SET name = ?, uri = ?, group_id = ?, rate = ?, sort = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		n.Name, n.URI, nullInt64(n.GroupID), n.Rate, n.Sort, boolInt(n.Enabled), now(), n.ID)
	return err
}

func (s *Store) DeleteExternalNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM external_nodes WHERE id = ?`, id)
	return err
}

// ---- node outbounds / routes ---------------------------------------------------------

// NodeRouting is a node's landing outbounds and rules.
type NodeRouting struct {
	Outbounds       []spec.Outbound  `json:"outbounds"`
	Routes          []spec.RouteRule `json:"routes"`
	DefaultOutbound string           `json:"default_outbound"`
	DNS             []string         `json:"dns"`
}

func (s *Store) NodeRouting(ctx context.Context, nodeID int64) (*NodeRouting, error) {
	var outs, routes, def, dns string
	if err := s.db.QueryRowContext(ctx, `SELECT outbounds_json, routes_json, default_outbound, dns_json FROM nodes WHERE id = ?`, nodeID).Scan(&outs, &routes, &def, &dns); err != nil {
		return nil, wrapNotFound(err)
	}
	nr := &NodeRouting{Outbounds: []spec.Outbound{}, Routes: []spec.RouteRule{}, DefaultOutbound: def, DNS: []string{}}
	_ = json.Unmarshal([]byte(outs), &nr.Outbounds)
	_ = json.Unmarshal([]byte(routes), &nr.Routes)
	_ = json.Unmarshal([]byte(dns), &nr.DNS)
	return nr, nil
}

func (s *Store) SetNodeRouting(ctx context.Context, nodeID int64, nr *NodeRouting) error {
	outs, _ := json.Marshal(nr.Outbounds)
	routes, _ := json.Marshal(nr.Routes)
	if nr.Outbounds == nil {
		outs = []byte("[]")
	}
	if nr.Routes == nil {
		routes = []byte("[]")
	}
	dns, _ := json.Marshal(nr.DNS)
	if nr.DNS == nil {
		dns = []byte("[]")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET outbounds_json = ?, routes_json = ?, default_outbound = ?, dns_json = ?, updated_at = ? WHERE id = ?`, string(outs), string(routes), nr.DefaultOutbound, string(dns), now(), nodeID)
	return err
}

// OverrideCores are the cores whose rendered config accepts an override.
var OverrideCores = []string{"xray", "singbox", "hysteria", "mita"}

// NodeOverrides returns the per-core override JSON text for a node.
func (s *Store) NodeOverrides(ctx context.Context, nodeID int64) (map[string]string, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT overrides_json FROM nodes WHERE id = ?`, nodeID).Scan(&raw); err != nil {
		return nil, wrapNotFound(err)
	}
	out := map[string]string{}
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &m)
	for _, c := range OverrideCores {
		if v, ok := m[c]; ok && len(v) > 0 {
			out[c] = string(v)
		} else {
			out[c] = ""
		}
	}
	return out, nil
}

// SetNodeOverrides validates each entry as a JSON object and stores them.
func (s *Store) SetNodeOverrides(ctx context.Context, nodeID int64, in map[string]string) error {
	m := map[string]json.RawMessage{}
	for _, c := range OverrideCores {
		raw := strings.TrimSpace(in[c])
		if raw == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return fmt.Errorf("%s: override must be a JSON object: %w", c, err)
		}
		m[c] = json.RawMessage(raw)
	}
	b, _ := json.Marshal(m)
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET overrides_json = ?, updated_at = ? WHERE id = ?`, string(b), now(), nodeID)
	return err
}
