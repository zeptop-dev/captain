package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type NetworkAlertPolicy struct {
	Mode          string  `json:"mode"`
	FixedMS       float64 `json:"fixed_ms"`
	DeviationPct  float64 `json:"deviation_pct"`
	FloorMS       float64 `json:"floor_ms"`
	LossPct       float64 `json:"loss_pct"`
	MinSamples    int     `json:"min_samples"`
	WindowMinutes int     `json:"window_minutes"`
}

func DefaultNetworkAlertPolicy() NetworkAlertPolicy {
	return NetworkAlertPolicy{Mode: "off", FixedMS: 300, DeviationPct: 50, FloorMS: 20, LossPct: 20, MinSamples: 20, WindowMinutes: 5}
}
func (p NetworkAlertPolicy) Validate() error {
	if !slices.Contains([]string{"off", "fixed", "adaptive"}, p.Mode) || p.FixedMS < 1 || p.FixedMS > 60000 || p.DeviationPct < 10 || p.DeviationPct > 1000 || p.FloorMS < 1 || p.FloorMS > 60000 || p.LossPct < 1 || p.LossPct > 100 || p.MinSamples < 5 || p.MinSamples > 100 || p.WindowMinutes < 2 || p.WindowMinutes > 30 {
		return ErrAssetInput
	}
	for _, v := range []float64{p.FixedMS, p.DeviationPct, p.FloorMS, p.LossPct} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ErrAssetInput
		}
	}
	return nil
}
func (s *Store) NetworkAlertPolicy(ctx context.Context, node int64) (NetworkAlertPolicy, error) {
	p := DefaultNetworkAlertPolicy()
	var raw string
	e := s.db.QueryRowContext(ctx, "SELECT policy_json FROM node_network_alerts WHERE node_id=?", node).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	e = json.Unmarshal([]byte(raw), &p)
	return p, e
}
func (s *Store) SetNetworkAlertPolicy(ctx context.Context, node int64, p NetworkAlertPolicy) error {
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
	e = tx.QueryRowContext(ctx, "SELECT policy_json FROM node_network_alerts WHERE node_id=?", node).Scan(&old)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if old == string(b) {
		return nil
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO node_network_alerts(node_id,policy_json,updated_at) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET policy_json=excluded.policy_json,updated_at=excluded.updated_at`, node, string(b), now()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM node_network_baselines WHERE node_id=?", node); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended_at=?,updated_at=?,resolution='disabled' WHERE node_id=? AND ended_at IS NULL AND kind LIKE 'network_%'`, now(), now(), node); e != nil {
		return e
	}
	return tx.Commit()
}

type networkSample struct {
	At int64   `json:"at"`
	MS float64 `json:"ms"`
}
type NetworkAlertState struct {
	LossReady    bool            `json:"loss_ready"`
	Current      bool            `json:"current"`
	Key          string          `json:"key"`
	Name         string          `json:"name"`
	Family       string          `json:"family"`
	Series       string          `json:"series"`
	TaskID       int64           `json:"task_id"`
	Epoch        string          `json:"epoch"`
	Interval     int             `json:"interval"`
	PlanAt       int64           `json:"plan_at"`
	Baseline     float64         `json:"baseline_ms"`
	Learned      int             `json:"learned"`
	First        int64           `json:"first"`
	Last         int64           `json:"last"`
	Average      float64         `json:"average_ms"`
	Loss         float64         `json:"loss_pct"`
	Threshold    float64         `json:"threshold_ms"`
	Ready        bool            `json:"ready"`
	Frozen       bool            `json:"frozen"`
	LatencyAlert bool            `json:"latency_alert"`
	LossAlert    bool            `json:"loss_alert"`
	HoldUntil    int64           `json:"hold_until"`
	Samples      []networkSample `json:"samples,omitempty"`
}

func networkKey(p spec.PingResult, planAt int64) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%d", p.TaskID, p.Name, p.Series, p.Family, p.Quality.Type, planAt))))
}
func recordNetworkSampleTx(ctx context.Context, tx *sql.Tx, node int64, p spec.PingResult, m spec.ProbeMeasurement, sampleAt, at time.Time, plan EffectiveProbePlan, policy NetworkAlertPolicy, policyAt, taskChanged int64) error {
	if policy.Mode == "off" || p.Quality == nil || p.Series == "" || sampleAt.Unix() <= policyAt || sampleAt.After(at) || at.Sub(sampleAt) > 2*time.Minute {
		return nil
	}
	if p.TaskID == 0 && !currentCarrier(plan, p.Name, p.Series, p.Family, sampleAt.Unix()) {
		return nil
	}
	if p.TaskID != 0 && !strings.HasPrefix(p.Series, "task:") {
		return nil
	}
	if m.Outcome != "ok" && m.Outcome != "timeout" && m.Outcome != "refused" {
		return nil
	}
	bindingAt := plan.ChangedAt
	if p.TaskID != 0 {
		bindingAt = taskChanged
		if sampleAt.Unix()*1000 <= taskChanged {
			return nil
		}
	}
	key := networkKey(p, bindingAt)
	var v NetworkAlertState
	var raw string
	e := tx.QueryRowContext(ctx, "SELECT state_json FROM node_network_baselines WHERE node_id=? AND series_key=?", node, key).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if e == nil {
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return e
		}
	}
	interval := max(1, p.Quality.IntervalSeconds)
	if v.Epoch != p.Quality.Epoch || v.Last > 0 && sampleAt.Unix()-v.Last > int64(max(120, 3*interval)) {
		v = NetworkAlertState{Key: key, Name: p.Name, Series: p.Series, Family: p.Family, TaskID: p.TaskID, Epoch: p.Quality.Epoch, Interval: interval, PlanAt: bindingAt}
	}
	if sampleAt.Unix() <= v.Last {
		return nil
	}
	v.Last = sampleAt.Unix()
	if v.First == 0 {
		v.First = v.Last
	}
	v.Samples = append(v.Samples, networkSample{v.Last, m.LatencyMs})
	cut := v.Last - int64(policy.WindowMinutes*60)
	keep := 0
	for keep < len(v.Samples) && v.Samples[keep].At < cut {
		keep++
	}
	v.Samples = v.Samples[keep:]
	if len(v.Samples) > 600 {
		v.Samples = v.Samples[len(v.Samples)-600:]
	}
	good, sum := 0, 0.0
	for _, x := range v.Samples {
		if x.MS >= 0 {
			good++
			sum += x.MS
		}
	}
	v.Loss = 100 * float64(len(v.Samples)-good) / float64(len(v.Samples))
	if good > 0 {
		v.Average = sum / float64(good)
	}
	covered := len(v.Samples) >= max(policy.MinSamples, int(math.Ceil(.8*float64(policy.WindowMinutes*60)/float64(interval)))) && v.Last-v.Samples[0].At >= int64(policy.WindowMinutes*60)-int64(2*interval)
	if v.Baseline == 0 && good >= policy.MinSamples && covered && v.Loss == 0 {
		values := make([]float64, 0, good)
		for _, x := range v.Samples {
			if x.MS >= 0 {
				values = append(values, x.MS)
			}
		}
		slices.Sort(values)
		v.Baseline = max(.1, values[len(values)/2])
		v.Learned = good
	}
	v.Ready = covered && (policy.Mode == "fixed" || v.Baseline > 0)
	v.LossReady = covered
	v.Threshold = policy.FixedMS
	if policy.Mode == "adaptive" {
		v.Threshold = v.Baseline + max(policy.FloorMS, v.Baseline*policy.DeviationPct/100)
	}
	if covered {
		before := v.LatencyAlert || v.LossAlert
		// Loss can be judged even with no successful latency samples; never invent latency recovery.
		if v.Ready && good >= policy.MinSamples {
			v.LatencyAlert = v.Average > v.Threshold
		}
		v.LossAlert = v.Loss >= policy.LossPct
		v.Frozen = v.LatencyAlert || v.LossAlert
		if before && !v.Frozen {
			v.HoldUntil = v.Last + int64(policy.WindowMinutes*60)
		}
		if policy.Mode == "adaptive" && !v.Frozen && v.Last >= v.HoldUntil && v.Loss == 0 && m.LatencyMs >= 0 && m.LatencyMs <= v.Threshold {
			v.Baseline = .99*v.Baseline + .01*m.LatencyMs
			v.Learned++
		}
	}
	b, _ := json.Marshal(v)
	_, e = tx.ExecContext(ctx, `INSERT INTO node_network_baselines(node_id,series_key,state_json) VALUES(?,?,?) ON CONFLICT(node_id,series_key) DO UPDATE SET state_json=excluded.state_json`, node, key, string(b))
	return e
}
func (s *Store) NetworkAlertStates(ctx context.Context, node int64) ([]NetworkAlertState, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT state_json FROM node_network_baselines WHERE node_id=? LIMIT 200", node)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []NetworkAlertState{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var v NetworkAlertState
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return nil, e
		}
		v.Samples = nil
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) MonitorKindMuted(ctx context.Context, node int64, kind string, at time.Time) (bool, error) {
	var n int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_kind_silences WHERE node_id=? AND kind=? AND (until_at=0 OR until_at>?)`, node, kind, at.Unix()).Scan(&n)
	return n > 0, e
}
func (s *Store) SilenceIncident(ctx context.Context, id int64, seconds int64, at time.Time) error {
	if seconds != 0 && seconds != 86400 && seconds != 259200 && seconds != 604800 && seconds != -1 {
		return ErrMonitorWindow
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var node int64
	var kind string
	if e = tx.QueryRowContext(ctx, "SELECT node_id,kind FROM monitor_incidents WHERE id=?", id).Scan(&node, &kind); e != nil {
		return wrapNotFound(e)
	}
	if seconds == -1 {
		_, e = tx.ExecContext(ctx, "DELETE FROM monitor_kind_silences WHERE node_id=? AND kind=?", node, kind)
	} else {
		until := int64(0)
		if seconds > 0 {
			until = at.Unix() + seconds
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO monitor_kind_silences(node_id,kind,until_at) VALUES(?,?,?) ON CONFLICT(node_id,kind) DO UPDATE SET until_at=excluded.until_at`, node, kind, until)
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}

func CurrentCarrierAlert(plan EffectiveProbePlan, name, series, family string, at int64) bool {
	return currentCarrier(plan, name, series, family, at)
}

func (s *Store) CurrentTaskAlert(ctx context.Context, task int64, generation int64) (bool, error) {
	var changed int64
	e := s.db.QueryRowContext(ctx, "SELECT alert_changed_at FROM ping_tasks WHERE id=?", task).Scan(&changed)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return changed == generation, e
}
