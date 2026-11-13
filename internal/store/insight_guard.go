package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Recheck the source inside the incident transaction. A configuration edit or a
// newer unknown sample between evaluation and claim must not revive old alerts.
func insightCurrentTx(ctx context.Context, tx *sql.Tx, node int64, c IncidentCheck) (bool, error) {
	if c.SourceKind == "" {
		return true, nil
	}
	if c.SourceKind == "network" {
		var raw, policyRaw string
		e := tx.QueryRowContext(ctx, "SELECT state_json FROM node_network_baselines WHERE node_id=? AND series_key=?", node, c.SourceKey).Scan(&raw)
		if e == sql.ErrNoRows {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		var v NetworkAlertState
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return false, e
		}
		ready := v.Ready
		if strings.HasPrefix(c.Kind, "network_loss:") {
			ready = v.LossReady
		}
		if v.Last != c.SourceAt || !ready {
			return false, nil
		}
		e = tx.QueryRowContext(ctx, "SELECT policy_json FROM node_network_alerts WHERE node_id=?", node).Scan(&policyRaw)
		if e == sql.ErrNoRows {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		var p NetworkAlertPolicy
		if e = json.Unmarshal([]byte(policyRaw), &p); e != nil {
			return false, e
		}
		if p.Mode == "off" {
			return false, nil
		}
		if v.TaskID != 0 {
			var changed int64
			e = tx.QueryRowContext(ctx, "SELECT alert_changed_at FROM ping_tasks WHERE id=?", v.TaskID).Scan(&changed)
			if e == sql.ErrNoRows {
				return false, nil
			}
			return changed == v.PlanAt, e
		}
		global, e := readProbeSettings(ctx, tx)
		if e != nil {
			return false, e
		}
		local, e := readNodeProbePlan(ctx, tx, node)
		if e != nil {
			return false, e
		}
		plan := ResolveNodeProbePlan(global, local)
		return plan.ChangedAt == v.PlanAt && currentCarrier(plan, v.Name, v.Series, v.Family, v.Last), nil
	}
	if c.SourceKind == "route" {
		var raw, policyRaw, scheduleRaw string
		e := tx.QueryRowContext(ctx, "SELECT state_json FROM node_route_states WHERE node_id=? AND path_key=?", node, c.SourceKey).Scan(&raw)
		if e == sql.ErrNoRows {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		var v routeState
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return false, e
		}
		if v.Last != c.SourceAt || v.Current != v.Candidate {
			return false, nil
		}
		e = tx.QueryRowContext(ctx, "SELECT policy_json FROM node_route_watch WHERE node_id=?", node).Scan(&policyRaw)
		if e == sql.ErrNoRows {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		var p RouteWatchPolicy
		if e = json.Unmarshal([]byte(policyRaw), &p); e != nil {
			return false, e
		}
		parts := strings.Split(c.SourceKey, "|")
		if len(parts) != 4 || !p.Notify || p.Expected[parts[2]] == "" || v.Count < p.Confirmations {
			return false, nil
		}
		e = tx.QueryRowContext(ctx, "SELECT config_json FROM node_route_schedules WHERE node_id=?", node).Scan(&scheduleRaw)
		if e == sql.ErrNoRows {
			return parts[0] == "beijing" && parts[1] == "ipv4", nil
		}
		if e != nil {
			return false, e
		}
		var schedule RouteSchedule
		if e = json.Unmarshal([]byte(scheduleRaw), &schedule); e != nil {
			return false, e
		}
		return parts[0] == schedule.City && parts[1] == schedule.Family, nil
	}
	return false, nil
}
func (s *Store) IncidentNoticeAllowed(ctx context.Context, id int64) (bool, error) {
	var resolution string
	e := s.db.QueryRowContext(ctx, "SELECT resolution FROM monitor_incidents WHERE id=?", id).Scan(&resolution)
	if e == sql.ErrNoRows {
		return false, nil
	}
	return resolution != "disabled", e
}
