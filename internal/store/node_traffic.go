package store

import (
	"context"
	"encoding/json"
	"time"
)

// NodeHostTraffic is the observed NIC usage in the saved monitoring period.
// It is independent of both customer billing and the quota's counting mode.
// Null timestamps and Valid=false mean unavailable, not measured zero.
type NodeHostTraffic struct {
	Valid      bool   `json:"valid"`
	UsedUp     int64  `json:"used_up"`
	UsedDown   int64  `json:"used_down"`
	CycleStart *int64 `json:"cycle_start"`
	CycleEnd   *int64 `json:"cycle_end"`
	UpdatedAt  *int64 `json:"updated_at"`
}

func hostTrafficView(p *NodeProbe) *NodeHostTraffic {
	v := &NodeHostTraffic{UsedUp: p.UsedUp, UsedDown: p.UsedDown}
	if p.PeriodStart.IsZero() {
		return v
	}
	start := p.PeriodStart.Unix()
	end := nodePeriodEnd(p.PeriodStart.In(time.Local), p).Unix()
	v.CycleStart, v.CycleEnd = &start, &end
	// Positive legacy usage is real even though older panels did not persist
	// the last metering timestamp. Never fabricate one from last_seen_at.
	v.Valid = p.UsedUp > 0 || p.UsedDown > 0
	if p.counters.TrafficKnown && p.counters.TrafficAt >= start {
		v.Valid = true
		at := p.counters.TrafficAt
		v.UpdatedAt = &at
	}
	return v
}

// NodeHostTrafficViews uses one small query for the server list. A nonzero ID
// selects a single server; unknown IDs are omitted from the returned map.
func (s *Store) NodeHostTrafficViews(ctx context.Context, nodeID int64) (map[int64]*NodeHostTraffic, error) {
	query := `SELECT id,probe_info_json,traffic_reset_day,traffic_period_start,traffic_used_up,traffic_used_down,probe_counters_json FROM nodes`
	var args []any
	if nodeID != 0 {
		query += ` WHERE id=?`
		args = append(args, nodeID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*NodeHostTraffic{}
	for rows.Next() {
		var p NodeProbe
		var info, counters string
		var start int64
		if err := rows.Scan(&p.NodeID, &info, &p.ResetDay, &start, &p.UsedUp, &p.UsedDown, &counters); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(info), &p.Info); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(counters), &p.counters); err != nil {
			return nil, err
		}
		if start > 0 {
			p.PeriodStart = time.Unix(start, 0)
		}
		out[p.NodeID] = hostTrafficView(&p)
	}
	return out, rows.Err()
}
