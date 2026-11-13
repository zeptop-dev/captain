package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type RouteWatchPolicy struct {
	Expected      map[string]string `json:"expected"`
	Confirmations int               `json:"confirmations"`
	Notify        bool              `json:"notify"`
}

var routeClasses = []string{"cn2", "163", "9929", "cug", "4837", "cmin2", "cmi", "cmnet", "ctgnet", "mixed", "unknown", "observed", "possible_gia"}

func (p RouteWatchPolicy) Validate() error {
	if p.Expected == nil || p.Confirmations < 1 || p.Confirmations > 10 || len(p.Expected) > 3 {
		return ErrAssetInput
	}
	for k, v := range p.Expected {
		if !slices.Contains([]string{"CT", "CU", "CM"}, k) || !slices.Contains(routeClasses, v) {
			return ErrAssetInput
		}
	}
	return nil
}
func (s *Store) RouteWatch(ctx context.Context, node int64) (RouteWatchPolicy, error) {
	p := RouteWatchPolicy{Expected: map[string]string{}, Confirmations: 2}
	var raw string
	e := s.db.QueryRowContext(ctx, "SELECT policy_json FROM node_route_watch WHERE node_id=?", node).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	e = json.Unmarshal([]byte(raw), &p)
	return p, e
}
func (s *Store) SetRouteWatch(ctx context.Context, node int64, p RouteWatchPolicy) error {
	if e := p.Validate(); e != nil {
		return e
	}
	b, _ := json.Marshal(p)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old string
	e = tx.QueryRowContext(ctx, "SELECT policy_json FROM node_route_watch WHERE node_id=?", node).Scan(&old)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if old == string(b) {
		return nil
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO node_route_watch(node_id,policy_json,updated_at) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET policy_json=excluded.policy_json,updated_at=excluded.updated_at`, node, string(b), time.Now().Unix()); e != nil {
		return e
	}
	// A policy edit deliberately starts a new confirmation window. Old jobs are fenced below.
	if _, e = tx.ExecContext(ctx, `UPDATE node_jobs SET done_at=?,error='route watch policy changed' WHERE node_id=? AND route_check=1 AND done_at IS NULL`, now(), node); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended_at=?,updated_at=?,resolution='disabled' WHERE node_id=? AND kind LIKE 'route_%' AND ended_at IS NULL`, now(), now(), node); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM node_route_states WHERE node_id=?", node); e != nil {
		return e
	}
	return tx.Commit()
}

type RouteEvent struct {
	ID            int64  `json:"id"`
	At            int64  `json:"at"`
	Carrier       string `json:"carrier"`
	City          string `json:"city"`
	Family        string `json:"family"`
	Target        string `json:"target"`
	Before        string `json:"before"`
	After         string `json:"after"`
	Expected      string `json:"expected"`
	Kind          string `json:"kind"`
	Rules         string `json:"rules"`
	Confirmations int    `json:"confirmations"`
}
type routeState struct {
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
	Count     int    `json:"count"`
	Rules     string `json:"rules"`
	Created   int64  `json:"created"`
	Last      int64  `json:"last"`
}

func routeCarrier(s string) string {
	switch strings.ToLower(s) {
	case "ct", "telecom":
		return "CT"
	case "cu", "unicom":
		return "CU"
	case "cm", "mobile":
		return "CM"
	}
	return ""
}
func recordRouteWatchTx(ctx context.Context, tx *sql.Tx, node int64, job string, result json.RawMessage, errText string, at time.Time) error {
	var params string
	var created int64
	var route bool
	if e := tx.QueryRowContext(ctx, "SELECT params_json,created_at,route_check FROM node_jobs WHERE node_id=? AND id=?", node, job).Scan(&params, &created, &route); e != nil {
		return e
	}
	if !route {
		return nil
	}
	var req spec.DiagnosticRequest
	var r struct {
		Outcome   string                  `json:"outcome"`
		StartedAt int64                   `json:"started_at"`
		Route     *spec.ReturnRouteResult `json:"route"`
	}
	if json.Unmarshal([]byte(params), &req) != nil || req.Route == nil {
		return nil
	}
	p := RouteWatchPolicy{Confirmations: 2}
	var policyRaw string
	var updated int64
	e := tx.QueryRowContext(ctx, "SELECT policy_json,updated_at FROM node_route_watch WHERE node_id=?", node).Scan(&policyRaw, &updated)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if e == nil && json.Unmarshal([]byte(policyRaw), &p) != nil {
		return nil
	}
	if created <= updated || created < at.Add(-time.Duration(spec.DiagnosticTTLSeconds)*time.Second).Unix() {
		return nil
	}
	if json.Unmarshal(result, &r) != nil || r.Route == nil || errText != "" {
		return breakRouteCandidatesTx(ctx, tx, node, req.Route.City+"|"+req.Route.Family+"|", created, nil)
	}
	rr := r.Route
	if rr.City != req.Route.City || rr.Family != req.Route.Family || rr.RulesVersion == "" || len(rr.RulesVersion) > 128 || !spec.Plain(rr.RulesVersion) || len(rr.Paths) > 3 {
		return breakRouteCandidatesTx(ctx, tx, node, req.Route.City+"|"+req.Route.Family+"|", created, nil)
	}
	seen := map[string]bool{}
	for _, path := range rr.Paths {
		carrier := routeCarrier(path.Carrier)
		if carrier == "" || seen[carrier] {
			continue
		}
		seen[carrier] = true
		if net.ParseIP(path.Target) == nil {
			continue
		}
		key := rr.City + "|" + rr.Family + "|" + carrier + "|" + path.Target
		var state routeState
		var raw string
		e := tx.QueryRowContext(ctx, "SELECT state_json FROM node_route_states WHERE node_id=? AND path_key=?", node, key).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			if e = json.Unmarshal([]byte(raw), &state); e != nil {
				return e
			}
		}
		// Late reports, unknown results and gaps cannot confirm a consecutive change.
		if created <= state.Created {
			continue
		}
		if path.Classification == "observed" && len(path.Evidence) == 1 && slices.Contains(routeClasses, path.Evidence[0].Code) {
			path.Classification = path.Evidence[0].Code
		}
		valid := path.Reached && path.Outcome == "ok" && path.ASNMissing == 0 && slices.Contains(routeClasses, path.Classification) && path.Classification != "unknown"
		kind, before := "", state.Current
		if !valid {
			state.Candidate = ""
			state.Count = 0
		} else if state.Rules != "" && state.Rules != rr.RulesVersion {
			kind = "rules_changed"
			state.Current = ""
			state.Count = 0
			state.Candidate = ""
		} else if state.Last > 0 && created-state.Last > int64(25*time.Hour/time.Second) {
			state.Candidate = ""
			state.Count = 0
		}
		if valid {
			if state.Candidate == path.Classification {
				state.Count = min(10, state.Count+1)
			} else {
				state.Candidate = path.Classification
				state.Count = 1
			}
			if state.Count >= p.Confirmations && state.Current != path.Classification {
				if kind == "" {
					kind = "baseline"
					if state.Current != "" {
						kind = "changed"
						if p.Expected[carrier] == path.Classification {
							kind = "recovered"
						}
					}
				}
				state.Current = path.Classification
			}
		}
		state.Rules = rr.RulesVersion
		state.Created = created
		state.Last = created
		b, _ := json.Marshal(state)
		if _, e = tx.ExecContext(ctx, `INSERT INTO node_route_states(node_id,path_key,state_json) VALUES(?,?,?) ON CONFLICT(node_id,path_key) DO UPDATE SET state_json=excluded.state_json`, node, key, string(b)); e != nil {
			return e
		}
		if kind != "" {
			ev := RouteEvent{At: at.Unix(), Carrier: carrier, City: rr.City, Family: rr.Family, Target: path.Target, Before: before, After: path.Classification, Expected: p.Expected[carrier], Kind: kind, Rules: rr.RulesVersion, Confirmations: state.Count}
			data, _ := json.Marshal(ev)
			if _, e = tx.ExecContext(ctx, `INSERT INTO node_route_events(node_id,job_id,path_key,at,event_json) VALUES(?,?,?,?,?)`, node, job, key, at.Unix(), string(data)); e != nil {
				return e
			}
		}
	}
	if e = breakRouteCandidatesTx(ctx, tx, node, rr.City+"|"+rr.Family+"|", created, seen); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM node_route_events WHERE node_id=? AND (at<? OR id NOT IN (SELECT id FROM node_route_events WHERE node_id=? ORDER BY id DESC LIMIT 200))`, node, at.AddDate(0, 0, -180).Unix(), node)
	return e
}
func (s *Store) RouteEvents(ctx context.Context, node int64) ([]RouteEvent, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT id,event_json FROM node_route_events WHERE node_id=? ORDER BY id DESC LIMIT 100", node)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []RouteEvent{}
	for rows.Next() {
		var id int64
		var raw string
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		var v RouteEvent
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return nil, e
		}
		v.ID = id
		out = append(out, v)
	}
	return out, rows.Err()
}
func routeIncidentKind(carrier string) string { return "route_" + strings.ToLower(carrier) }

func (s *Store) RouteAlertChecks(ctx context.Context, node int64, at time.Time) ([]IncidentCheck, error) {
	policy, e := s.RouteWatch(ctx, node)
	if e != nil {
		return nil, e
	}
	schedule, e := s.RouteSchedule(ctx, node)
	if e != nil {
		return nil, e
	}
	checks := []IncidentCheck{}
	if !policy.Notify {
		return checks, nil
	}
	rows, e := s.db.QueryContext(ctx, "SELECT path_key,state_json FROM node_route_states WHERE node_id=? ORDER BY json_extract(state_json,'$.created') DESC", node)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var key, raw string
		if e = rows.Scan(&key, &raw); e != nil {
			return nil, e
		}
		parts := strings.Split(key, "|")
		if len(parts) != 4 || parts[0] != schedule.City || parts[1] != schedule.Family {
			continue
		}
		var v routeState
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return nil, e
		}
		if found[parts[2]] {
			continue
		}
		found[parts[2]] = true
		expected := policy.Expected[parts[2]]
		if expected == "" || v.Current == "" || v.Count < policy.Confirmations || v.Candidate != v.Current || at.Unix() < v.Last || at.Unix()-v.Last > int64((time.Duration(schedule.IntervalHours)*time.Hour+time.Hour)/time.Second) {
			continue
		}
		found[parts[2]] = true
		checks = append(checks, IncidentCheck{SourceKind: "route", SourceKey: key, SourceAt: v.Last, Kind: routeIncidentKind(parts[2]), Active: v.Current != expected, Value: float64(v.Count), Threshold: float64(policy.Confirmations)})
	}
	for _, c := range []string{"CT", "CU", "CM"} {
		if policy.Expected[c] == "" && !found[c] {
			checks = append(checks, IncidentCheck{Kind: routeIncidentKind(c), Resolution: "disabled"})
		}
	}
	return checks, rows.Err()
}

// Break only unconfirmed candidates; unavailable evidence is never recovery.
func breakRouteCandidatesTx(ctx context.Context, tx *sql.Tx, node int64, prefix string, created int64, seen map[string]bool) error {
	rows, e := tx.QueryContext(ctx, "SELECT path_key,state_json FROM node_route_states WHERE node_id=? AND path_key LIKE ?", node, prefix+"%")
	if e != nil {
		return e
	}
	type item struct {
		key   string
		state routeState
	}
	var items []item
	for rows.Next() {
		var key, raw string
		if e = rows.Scan(&key, &raw); e != nil {
			rows.Close()
			return e
		}
		parts := strings.Split(key, "|")
		if len(parts) != 4 || seen[parts[2]] {
			continue
		}
		var state routeState
		if e = json.Unmarshal([]byte(raw), &state); e != nil {
			rows.Close()
			return e
		}
		if state.Created >= created {
			continue
		}
		state.Count = 0
		state.Candidate = ""
		state.Created = created
		items = append(items, item{key, state})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range items {
		b, _ := json.Marshal(v.state)
		if _, e = tx.ExecContext(ctx, "UPDATE node_route_states SET state_json=? WHERE node_id=? AND path_key=?", string(b), node, v.key); e != nil {
			return e
		}
	}
	return nil
}
