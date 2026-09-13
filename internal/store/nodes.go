package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/domain"
)

const nodeCols = "id, name, token_hash, pair_code, public_addr, internal_addr, v6_addr, domain, monitor_url, version, platform, hostname, last_seen_at, applied_revision, upgrade_to, created_at"

func scanNode(row interface{ Scan(...any) error }) (*domain.Node, error) {
	var n domain.Node
	var tokenHash, pairCode sql.NullString
	var lastSeen sql.NullInt64
	var created int64
	if err := row.Scan(&n.ID, &n.Name, &tokenHash, &pairCode, &n.PublicAddr, &n.InternalAddr, &n.V6Addr, &n.Domain, &n.MonitorURL,
		&n.Version, &n.Platform, &n.Hostname, &lastSeen, &n.AppliedRevision, &n.UpgradeTo, &created); err != nil {
		return nil, wrapNotFound(err)
	}
	n.Paired = tokenHash.Valid && tokenHash.String != ""
	n.PairCode = pairCode.String
	n.LastSeenAt = unixPtr(lastSeen)
	n.CreatedAt = unix(created)
	return &n, nil
}

// CreateNode inserts a node with a fresh pairing code valid for ttl.
func (s *Store) CreateNode(ctx context.Context, n *domain.Node, pairCode string, ttl time.Duration) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO nodes (name, pair_code, pair_code_expires_at, public_addr, internal_addr, v6_addr, domain, monitor_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, pairCode, time.Now().Add(ttl).Unix(), n.PublicAddr, n.InternalAddr, n.V6Addr, n.Domain, n.MonitorURL, ts, ts)
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	n.PairCode = pairCode
	n.CreatedAt = unix(ts)
	return nil
}

func (s *Store) NodeByID(ctx context.Context, id int64) (*domain.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id = ?`, id))
}

func (s *Store) NodeByTokenHash(ctx context.Context, hash string) (*domain.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE token_hash = ?`, hash))
}

// RedeemPairCode consumes a valid pairing code: sets the token hash, clears
// the code, records agent details. Returns the node or ErrNotFound.
func (s *Store) RedeemPairCode(ctx context.Context, code, tokenHash, hostname, version, platform string) (*domain.Node, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET token_hash = ?, pair_code = NULL, pair_code_expires_at = NULL,
		hostname = ?, version = ?, platform = ?, last_seen_at = ?, updated_at = ?
		WHERE pair_code = ? AND pair_code_expires_at > ? AND token_hash IS NULL`,
		tokenHash, hostname, version, platform, now(), now(), code, now())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.NodeByTokenHash(ctx, tokenHash)
}

func (s *Store) ListNodes(ctx context.Context) ([]*domain.Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeCols+` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// TouchNode records a report: liveness, applied revision, host and core status.
func (s *Store) TouchNode(ctx context.Context, id int64, version, revision string, host spec.SystemStatus, cores any, certs any) error {
	hostJSON, _ := json.Marshal(host)
	coresJSON, _ := json.Marshal(cores)
	certsJSON, _ := json.Marshal(certs)
	if certs == nil {
		certsJSON = []byte("[]")
	}
	// A node that reports the requested release has finished upgrading.
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen_at = ?, version = COALESCE(NULLIF(?, ''), version), applied_revision = ?, host_status_json = ?, cores_json = ?, certs_json = ?,
		upgrade_to = CASE WHEN upgrade_to = ? THEN '' ELSE upgrade_to END, updated_at = ? WHERE id = ?`,
		now(), version, revision, string(hostJSON), string(coresJSON), string(certsJSON), version, now(), id)
	return err
}

const inboundCols = "id, node_id, tag, protocol, listen, port, core, settings_json, group_id, enabled, sort, ingress_id"

func inboundColsPrefixed(p string) string {
	return p + ".id, " + p + ".node_id, " + p + ".tag, " + p + ".protocol, " + p + ".listen, " + p + ".port, " + p + ".core, " + p + ".settings_json, " + p + ".group_id, " + p + ".enabled, " + p + ".sort, " + p + ".ingress_id"
}

func unmarshalSettings(raw string, ib *domain.Inbound) error {
	return json.Unmarshal([]byte(raw), &ib.Settings)
}

func scanInbound(row interface{ Scan(...any) error }) (*domain.Inbound, error) {
	var ib domain.Inbound
	var settings string
	var group, ingress sql.NullInt64
	var enabled int
	if err := row.Scan(&ib.ID, &ib.NodeID, &ib.Tag, &ib.Protocol, &ib.Listen, &ib.Port, &ib.Core, &settings, &group, &enabled, &ib.Sort, &ingress); err != nil {
		return nil, wrapNotFound(err)
	}
	if err := json.Unmarshal([]byte(settings), &ib.Settings); err != nil {
		return nil, err
	}
	ib.GroupID, ib.IngressID = int64Ptr(group), int64Ptr(ingress)
	ib.Enabled = enabled == 1
	return &ib, nil
}

func (s *Store) CreateInbound(ctx context.Context, ib *domain.Inbound) error {
	settings, err := json.Marshal(ib.Settings)
	if err != nil {
		return err
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO inbounds (node_id, tag, protocol, listen, port, core, settings_json, group_id, enabled, sort, ingress_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ib.NodeID, ib.Tag, ib.Protocol, ib.Listen, ib.Port, ib.Core, string(settings), nullInt64(ib.GroupID), boolInt(ib.Enabled), ib.Sort, nullInt64(ib.IngressID), ts, ts)
	if err != nil {
		return err
	}
	ib.ID, _ = res.LastInsertId()
	return nil
}

// InboundsByNode lists enabled inbounds of a node.
func (s *Store) InboundsByNode(ctx context.Context, nodeID int64) ([]*domain.Inbound, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inboundCols+` FROM inbounds WHERE node_id = ? AND enabled = 1 ORDER BY sort, id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Inbound
	for rows.Next() {
		ib, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ib)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateNode changes editable node fields.
func (s *Store) UpdateNode(ctx context.Context, n *domain.Node) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET name = ?, public_addr = ?, internal_addr = ?, v6_addr = ?, domain = ?, monitor_url = ?, updated_at = ? WHERE id = ?`,
		n.Name, n.PublicAddr, n.InternalAddr, n.V6Addr, n.Domain, n.MonitorURL, now(), n.ID)
	return err
}

// ResetPairCode issues a fresh pairing code and revokes the current token.
func (s *Store) ResetPairCode(ctx context.Context, id int64, code string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET pair_code = ?, pair_code_expires_at = ?, token_hash = NULL, updated_at = ? WHERE id = ?`,
		code, time.Now().Add(ttl).Unix(), now(), id)
	return err
}

func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	return err
}

// NodeStatus is the last reported host/core state.
type NodeStatus struct {
	Host  json.RawMessage `json:"host"`
	Cores json.RawMessage `json:"cores"`
	Certs json.RawMessage `json:"certs"`
}

func (s *Store) NodeStatus(ctx context.Context, id int64) (*NodeStatus, error) {
	var host, cores, certs string
	if err := s.db.QueryRowContext(ctx, `SELECT host_status_json, cores_json, certs_json FROM nodes WHERE id = ?`, id).Scan(&host, &cores, &certs); err != nil {
		return nil, wrapNotFound(err)
	}
	if certs == "" {
		certs = "[]"
	}
	return &NodeStatus{Host: json.RawMessage(host), Cores: json.RawMessage(cores), Certs: json.RawMessage(certs)}, nil
}

// AllInboundsByNode lists inbounds of a node including disabled ones.
func (s *Store) AllInboundsByNode(ctx context.Context, nodeID int64) ([]*domain.Inbound, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inboundCols+` FROM inbounds WHERE node_id = ? ORDER BY sort, id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Inbound
	for rows.Next() {
		ib, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ib)
	}
	return out, rows.Err()
}

func (s *Store) InboundByID(ctx context.Context, id int64) (*domain.Inbound, error) {
	return scanInbound(s.db.QueryRowContext(ctx, `SELECT `+inboundCols+` FROM inbounds WHERE id = ?`, id))
}

func (s *Store) UpdateInbound(ctx context.Context, ib *domain.Inbound) error {
	settings, err := json.Marshal(ib.Settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE inbounds SET tag = ?, protocol = ?, listen = ?, port = ?, core = ?, settings_json = ?, group_id = ?, enabled = ?, sort = ?, ingress_id = ?, updated_at = ? WHERE id = ?`,
		ib.Tag, ib.Protocol, ib.Listen, ib.Port, ib.Core, string(settings), nullInt64(ib.GroupID), boolInt(ib.Enabled), ib.Sort, nullInt64(ib.IngressID), now(), ib.ID)
	return err
}

func (s *Store) DeleteInbound(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM inbounds WHERE id = ?`, id)
	return err
}

// NodeTrafficToday sums today's traffic per node from daily buckets.
func (s *Store) NodeTrafficToday(ctx context.Context, day time.Time) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.node_id, SUM(t.up_bytes + t.down_bytes) FROM traffic_daily t JOIN inbounds i ON i.id = t.inbound_id WHERE t.day = ? GROUP BY i.node_id`,
		day.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, sum int64
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, err
		}
		out[id] = sum
	}
	return out, rows.Err()
}

// SetNodeUpgrade records the release a node should upgrade to ("" cancels).
func (s *Store) SetNodeUpgrade(ctx context.Context, id int64, version string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET upgrade_to = ?, updated_at = ? WHERE id = ?`, version, now(), id)
	return err
}

// SetAllNodesUpgrade asks every paired node not already on version to upgrade.
func (s *Store) SetAllNodesUpgrade(ctx context.Context, version string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET upgrade_to = ?, updated_at = ? WHERE token_hash <> '' AND version <> ?`, version, now(), version)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CertProblems returns, per node, whether any automatic certificate has an
// error or expires within 14 days.
func (s *Store) CertProblems(ctx context.Context, at time.Time) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, certs_json FROM nodes WHERE certs_json <> '' AND certs_json <> '[]'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var certs []agentproto.CertStatus
		_ = json.Unmarshal([]byte(raw), &certs)
		for _, c := range certs {
			if c.Error != "" || (!c.NotAfter.IsZero() && c.NotAfter.Before(at.Add(14*24*time.Hour))) {
				out[id] = true
			}
		}
	}
	return out, rows.Err()
}

// PairCodeValid reports whether code can still be redeemed.
func (s *Store) PairCodeValid(ctx context.Context, code string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE pair_code = ? AND pair_code_expires_at > ? AND token_hash IS NULL`, code, now()).Scan(&n)
	return n > 0, err
}
