package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// NodeProxyTraffic records customer payload by its accepted exit attribution.
// Collection remains on the authenticating server and customer billing happens
// only once. The legacy traffic_today_bytes field keeps its existing meaning.
type NodeProxyTraffic struct {
	TodayBytes             int64 `json:"today_bytes"`
	CollectedTodayBytes    int64 `json:"collected_today_bytes"`
	ForwardedTodayBytes    int64 `json:"forwarded_today_bytes"`
	ReceivedTodayBytes     int64 `json:"received_today_bytes"`
	UnattributedTodayBytes int64 `json:"unattributed_today_bytes"`
	DayStart               int64 `json:"day_start"`
	DayEnd                 int64 `json:"day_end"`
	HistoryComplete        bool  `json:"history_complete"`
	AttributionStartedAt   int64 `json:"attribution_started_at"`
}

// NodeProxyTrafficViews does not consult live connections. Deleted collectors
// and inbounds leave previously accepted exit usage intact, while node-ID reuse
// cannot make a new server inherit either end of the old projection.
func (s *Store) NodeProxyTrafficViews(ctx context.Context, nodeID int64, at time.Time) (map[int64]*NodeProxyTraffic, error) {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	query := `SELECT n.id, p.attribution_started_at FROM nodes n JOIN node_proxy_identities p ON p.node_id = n.id`
	var args []any
	if nodeID != 0 {
		query += ` WHERE n.id = ?`
		args = append(args, nodeID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := map[int64]*NodeProxyTraffic{}
	for rows.Next() {
		var id int64
		var started string
		if err := rows.Scan(&id, &started); err != nil {
			rows.Close()
			return nil, err
		}
		start, err := time.Parse("2006-01-02 15:04:05", started)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = &NodeProxyTraffic{DayStart: day, DayEnd: day + 86400, AttributionStartedAt: start.Unix(), HistoryComplete: day >= start.Unix()}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	query = `SELECT c.node_id, e.node_id, t.up_bytes + t.down_bytes, t.unattributed_bytes
		FROM node_proxy_traffic_daily t
		JOIN node_proxy_identities c ON c.id = t.collector_id
		JOIN node_proxy_identities e ON e.id = t.exit_id WHERE t.day = ?`
	args = []any{day}
	if nodeID != 0 {
		query += ` AND (c.node_id = ? OR e.node_id = ?)`
		args = append(args, nodeID, nodeID)
	}
	rows, err = s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var collector, exit sql.NullInt64
		var total, unknown int64
		if err := rows.Scan(&collector, &exit, &total, &unknown); err != nil {
			return nil, err
		}
		if v := out[collector.Int64]; collector.Valid && v != nil {
			v.CollectedTodayBytes += total
			v.UnattributedTodayBytes += unknown
			if collector != exit {
				v.ForwardedTodayBytes += total
			}
		}
		if v := out[exit.Int64]; exit.Valid && v != nil {
			v.TodayBytes += total
			if collector != exit {
				v.ReceivedTodayBytes += total
			}
		}
	}
	return out, rows.Err()
}

func proxyIdentityTx(ctx context.Context, tx *sql.Tx, nodeID int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_proxy_identities(node_id) VALUES (?) ON CONFLICT(node_id) DO NOTHING`, nodeID); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM node_proxy_identities WHERE node_id = ?`, nodeID).Scan(&id)
	return id, err
}

// addProxyTrafficTx snapshots the collector/exit mapping in the same transaction
// as the charge and report receipt. The report's node is authoritative when the
// original inbound is unknown; the billing fallback must never guess an exit.
func addProxyTrafficTx(ctx context.Context, tx *sql.Tx, collectorNodeID int64, samples []TrafficSample, at time.Time) error {
	type destination struct {
		collector, exit int64
		unknown         bool
	}
	type counters struct{ up, down, unknown int64 }
	byInbound := map[int64]destination{}
	totals := map[destination]counters{}
	for _, sample := range samples {
		if sample.Up < 0 || sample.Down < 0 || sample.Up == 0 && sample.Down == 0 {
			continue
		}
		d, found := byInbound[sample.InboundID]
		if !found {
			err := tx.QueryRowContext(ctx, `SELECT i.node_id, COALESCE(w.exit_id, r.exit_id, i.node_id)
				FROM inbounds i
				LEFT JOIN node_wireguard_links w ON w.user_inbound_id = i.id AND w.transit_id = i.node_id
				LEFT JOIN node_reverse_links r ON r.user_inbound_id = i.id AND r.transit_id = i.node_id
				WHERE i.id = ?`, sample.InboundID).Scan(&d.collector, &d.exit)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if collectorNodeID != 0 && (err != nil || d.collector != collectorNodeID) {
				d = destination{collector: collectorNodeID, exit: collectorNodeID, unknown: true}
			}
			byInbound[sample.InboundID] = d
		}
		if d.collector == 0 {
			continue // historical direct store callers may have no known inbound
		}
		if sample.UnattributedInbound {
			d.exit, d.unknown = d.collector, true
		}
		t := totals[d]
		t.up += sample.Up
		t.down += sample.Down
		if d.unknown {
			t.unknown += sample.Up + sample.Down
		}
		totals[d] = t
	}
	identities := map[int64]int64{}
	identity := func(nodeID int64) (int64, error) {
		if id := identities[nodeID]; id != 0 {
			return id, nil
		}
		id, err := proxyIdentityTx(ctx, tx, nodeID)
		identities[nodeID] = id
		return id, err
	}
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	for d, t := range totals {
		collector, err := identity(d.collector)
		if err != nil {
			return err
		}
		exit, err := identity(d.exit)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_proxy_traffic_daily(collector_id, exit_id, day, up_bytes, down_bytes, unattributed_bytes)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(collector_id, exit_id, day) DO UPDATE SET
			up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes,
			unattributed_bytes = unattributed_bytes + excluded.unattributed_bytes`, collector, exit, day, t.up, t.down, t.unknown); err != nil {
			return err
		}
	}
	return nil
}
