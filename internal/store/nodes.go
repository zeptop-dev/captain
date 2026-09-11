package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gitlab.com/boyang-hu/bosun/pkg/spec"

	"gitlab.com/boyang-hu/captain/internal/domain"
)

const nodeCols = "id, name, token_hash, pair_code, public_addr, internal_addr, v6_addr, monitor_url, version, platform, hostname, last_seen_at, applied_revision, created_at"

func scanNode(row interface{ Scan(...any) error }) (*domain.Node, error) {
	var n domain.Node
	var tokenHash, pairCode sql.NullString
	var lastSeen sql.NullInt64
	var created int64
	if err := row.Scan(&n.ID, &n.Name, &tokenHash, &pairCode, &n.PublicAddr, &n.InternalAddr, &n.V6Addr, &n.MonitorURL,
		&n.Version, &n.Platform, &n.Hostname, &lastSeen, &n.AppliedRevision, &created); err != nil {
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
	res, err := s.db.ExecContext(ctx, `INSERT INTO nodes (name, pair_code, pair_code_expires_at, public_addr, internal_addr, v6_addr, monitor_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, pairCode, time.Now().Add(ttl).Unix(), n.PublicAddr, n.InternalAddr, n.V6Addr, n.MonitorURL, ts, ts)
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
func (s *Store) TouchNode(ctx context.Context, id int64, version, revision string, host spec.SystemStatus, cores any) error {
	hostJSON, _ := json.Marshal(host)
	coresJSON, _ := json.Marshal(cores)
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen_at = ?, version = COALESCE(NULLIF(?, ''), version), applied_revision = ?, host_status_json = ?, cores_json = ?, updated_at = ? WHERE id = ?`,
		now(), version, revision, string(hostJSON), string(coresJSON), now(), id)
	return err
}

const inboundCols = "id, node_id, tag, protocol, listen, port, core, settings_json, group_id, enabled, sort"

func inboundColsPrefixed(p string) string {
	return p + ".id, " + p + ".node_id, " + p + ".tag, " + p + ".protocol, " + p + ".listen, " + p + ".port, " + p + ".core, " + p + ".settings_json, " + p + ".group_id, " + p + ".enabled, " + p + ".sort"
}

func unmarshalSettings(raw string, ib *domain.Inbound) error {
	return json.Unmarshal([]byte(raw), &ib.Settings)
}

func scanInbound(row interface{ Scan(...any) error }) (*domain.Inbound, error) {
	var ib domain.Inbound
	var settings string
	var group sql.NullInt64
	var enabled int
	if err := row.Scan(&ib.ID, &ib.NodeID, &ib.Tag, &ib.Protocol, &ib.Listen, &ib.Port, &ib.Core, &settings, &group, &enabled, &ib.Sort); err != nil {
		return nil, wrapNotFound(err)
	}
	if err := json.Unmarshal([]byte(settings), &ib.Settings); err != nil {
		return nil, err
	}
	ib.GroupID = int64Ptr(group)
	ib.Enabled = enabled == 1
	return &ib, nil
}

func (s *Store) CreateInbound(ctx context.Context, ib *domain.Inbound) error {
	settings, err := json.Marshal(ib.Settings)
	if err != nil {
		return err
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO inbounds (node_id, tag, protocol, listen, port, core, settings_json, group_id, enabled, sort, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ib.NodeID, ib.Tag, ib.Protocol, ib.Listen, ib.Port, ib.Core, string(settings), nullInt64(ib.GroupID), boolInt(ib.Enabled), ib.Sort, ts, ts)
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
