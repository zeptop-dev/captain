package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
)

var ErrProbePlan = errors.New("invalid server probe plan")
var ErrProbePlanConflict = errors.New("server probe plan changed; reload before saving")

type NodeProbePlan struct {
	Mode      string                   `json:"mode"`
	Revision  string                   `json:"revision"`
	Custom    *spec.CarrierProbeConfig `json:"custom"`
	ChangedAt int64                    `json:"-"`
}
type EffectiveProbePlan struct {
	Enabled   bool                     `json:"enabled"`
	Source    string                   `json:"source"`
	Config    *spec.CarrierProbeConfig `json:"config"`
	ChangedAt int64                    `json:"changed_at"`
}
type ProbePlanDefault struct {
	Enabled bool                     `json:"enabled"`
	Config  *spec.CarrierProbeConfig `json:"config"`
}
type NodeProbePlanView struct {
	NodeProbePlan
	Effective         EffectiveProbePlan `json:"effective"`
	MonitoringEnabled bool               `json:"monitoring_enabled"`
	Global            ProbePlanDefault   `json:"global"`
}
type NodeProbePlanChange struct {
	Revision string                   `json:"revision"`
	Mode     string                   `json:"mode"`
	Custom   *spec.CarrierProbeConfig `json:"custom,omitempty"`
}

// ResolveNodeProbePlan keeps carrier selection separate from resource monitoring
// and independent ping tasks. Disabled and inherited plans retain their draft.
func ResolveNodeProbePlan(global ProbeSettings, plan NodeProbePlan) EffectiveProbePlan {
	global.Normalize()
	mode := plan.Mode
	if mode == "" {
		mode = "inherit"
	}
	out := EffectiveProbePlan{Enabled: global.Enabled && global.CarrierPing, Source: mode, Config: global.CarrierProbe.Clone(), ChangedAt: max(plan.ChangedAt, global.MonitoringChangedAt)}
	switch mode {
	case "inherit":
		out.ChangedAt = max(out.ChangedAt, global.CarrierChangedAt)
	case "custom":
		out.Enabled = global.Enabled && plan.Custom != nil
		if plan.Custom != nil {
			cfg := plan.Custom.Normalize()
			out.Config = &cfg
		}
	default:
		out.Enabled = false
	}
	return out
}
func probePlanView(global ProbeSettings, plan NodeProbePlan) NodeProbePlanView {
	global.Normalize()
	return NodeProbePlanView{NodeProbePlan: plan, Effective: ResolveNodeProbePlan(global, plan), MonitoringEnabled: global.Enabled, Global: ProbePlanDefault{Enabled: global.CarrierPing, Config: global.CarrierProbe.Clone()}}
}
func readNodeProbePlan(ctx context.Context, q rowQuerier, id int64) (NodeProbePlan, error) {
	var p NodeProbePlan
	var raw string
	err := q.QueryRowContext(ctx, `SELECT mode,revision,custom_json,changed_at FROM node_probe_plans WHERE node_id=?`, id).Scan(&p.Mode, &p.Revision, &raw, &p.ChangedAt)
	if err != nil {
		return p, wrapNotFound(err)
	}
	err = json.Unmarshal([]byte(raw), &p.Custom)
	return p, err
}
func readProbeSettings(ctx context.Context, q rowQuerier) (ProbeSettings, error) {
	var p ProbeSettings
	var raw string
	err := q.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbe).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}
func (s *Store) NodeProbePlan(ctx context.Context, id int64) (NodeProbePlan, error) {
	return readNodeProbePlan(ctx, s.db, id)
}
func (s *Store) ListNodeProbePlans(ctx context.Context) (map[int64]NodeProbePlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,mode,revision,custom_json,changed_at FROM node_probe_plans`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]NodeProbePlan{}
	for rows.Next() {
		var id int64
		var p NodeProbePlan
		var raw string
		if err := rows.Scan(&id, &p.Mode, &p.Revision, &raw, &p.ChangedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &p.Custom); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}
func (s *Store) NodeProbePlanView(ctx context.Context, id int64) (NodeProbePlanView, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	defer tx.Rollback()
	p, err := readNodeProbePlan(ctx, tx, id)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	g, err := readProbeSettings(ctx, tx)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	return probePlanView(g, p), tx.Commit()
}
func sameEffectiveProbePlan(a, b EffectiveProbePlan) bool {
	if a.Enabled != b.Enabled {
		return false
	}
	return !a.Enabled || reflect.DeepEqual(a.Config.Tasks(), b.Config.Tasks())
}
func (s *Store) UpdateNodeProbePlan(ctx context.Context, id int64, in NodeProbePlanChange, at time.Time) (NodeProbePlanView, error) {
	switch in.Mode {
	case "inherit", "custom", "disabled":
	default:
		return NodeProbePlanView{}, ErrProbePlan
	}
	if in.Custom != nil {
		if err := in.Custom.Validate(); err != nil {
			return NodeProbePlanView{}, fmt.Errorf("%w: %v", ErrProbePlan, err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	defer tx.Rollback()
	old, err := readNodeProbePlan(ctx, tx, id)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	if in.Revision != old.Revision {
		return NodeProbePlanView{}, ErrProbePlanConflict
	}
	global, err := readProbeSettings(ctx, tx)
	if err != nil {
		return NodeProbePlanView{}, err
	}
	global.Normalize()
	next := old
	next.Mode = in.Mode
	if in.Custom != nil {
		cfg := in.Custom.Normalize()
		next.Custom = &cfg
	}
	if next.Mode == "custom" && next.Custom == nil {
		next.Custom = global.CarrierProbe.Clone()
	}
	previous := ResolveNodeProbePlan(global, old)
	// Changing source cannot revive samples invalidated by an earlier global change.
	if old.Mode != next.Mode {
		next.ChangedAt = max(next.ChangedAt, previous.ChangedAt)
	}
	if !sameEffectiveProbePlan(previous, ResolveNodeProbePlan(global, next)) {
		next.ChangedAt = max(at.Unix(), previous.ChangedAt)
	}
	if old.Mode != next.Mode || !reflect.DeepEqual(old.Custom, next.Custom) {
		next.Revision = auth.Token(24)
		raw, err := json.Marshal(next.Custom)
		if err != nil {
			return NodeProbePlanView{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE node_probe_plans SET mode=?,revision=?,custom_json=?,changed_at=? WHERE node_id=?`, next.Mode, next.Revision, string(raw), next.ChangedAt, id); err != nil {
			return NodeProbePlanView{}, err
		}
	}
	return probePlanView(global, next), tx.Commit()
}

// setProbeSetting owns the server-generated epochs. Older clients can omit or
// echo them, but cannot reset history by supplying an arbitrary timestamp.
func (s *Store) setProbeSetting(ctx context.Context, raw []byte) error {
	next, nextErr := probePlanSettings(raw)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRaw string
	err = tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbe).Scan(&oldRaw)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	old, oldErr := probePlanSettings([]byte(oldRaw))
	next.MonitoringChangedAt, next.CarrierChangedAt = old.MonitoringChangedAt, old.CarrierChangedAt
	if exists {
		a, b := old, next
		a.Normalize()
		b.Normalize()
		if nextErr != nil || oldErr != nil || a.Enabled != b.Enabled {
			next.MonitoringChangedAt = max(time.Now().Unix(), old.MonitoringChangedAt)
		}
		if nextErr != nil || oldErr != nil || a.CarrierPing != b.CarrierPing || (b.CarrierPing && !reflect.DeepEqual(a.CarrierProbe.Tasks(), b.CarrierProbe.Tasks())) {
			next.CarrierChangedAt = max(time.Now().Unix(), old.CarrierChangedAt)
		}
	}
	// Retain unknown fields and original omission semantics of generic settings clients.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		// Preserve the generic JSON store contract even for a malformed whole
		// document. On repair, the undecodable old selection advances epochs.
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, SettingProbe, string(raw)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	fields["monitoring_changed_at"], _ = json.Marshal(next.MonitoringChangedAt)
	fields["carrier_changed_at"], _ = json.Marshal(next.CarrierChangedAt)
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, SettingProbe, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}
func currentCarrier(plan EffectiveProbePlan, name, series, family string, at int64) bool {
	if !plan.Enabled || plan.Config == nil || at <= plan.ChangedAt {
		return false
	}
	for _, task := range plan.Config.Tasks() {
		if task.Name == name && task.Series == series && task.Family == family {
			return true
		}
	}
	return false
}

// FilterCarrierSamples leaves separately assigned tasks untouched. Normalize
// host timestamps with MonitorSnapshot before calling this on a raw report.
func FilterCarrierSamples(host spec.SystemStatus, plan EffectiveProbePlan) spec.SystemStatus {
	out := make([]spec.PingResult, 0, len(host.Pings))
	for _, p := range host.Pings {
		if p.TaskID != 0 {
			out = append(out, p)
			continue
		}
		if host.CarrierProbeVersion < 1 || !currentCarrier(plan, p.Name, p.Series, p.Family, p.At) {
			continue
		}
		// A new latest sample cannot certify the previous rolling window.
		// Older agents lack this boundary, so keep their latest measurement
		// but leave the window unknown after an effective plan change.
		if plan.ChangedAt > 0 && (p.Quality == nil || p.Quality.WindowStartedAt <= plan.ChangedAt || p.Quality.WindowStartedAt > p.At) {
			p.Loss = 0
			if p.Quality != nil {
				q := *p.Quality
				q.Window = spec.ProbeWindow{}
				p.Quality = &q
			}
		}
		out = append(out, p)
	}
	host.Pings = out
	return host
}

// Historical buckets keep their actual timestamps. Selecting the same exact
// protocol/family/target again makes its previous history visible, while current
// values and rolling windows still require fresh evidence after the change.
func FilterCarrierPoints(points []PingPoint, plan EffectiveProbePlan) []PingPoint {
	plan.ChangedAt = 0 // History is not a claim that this plan is currently applied.
	out := make([]PingPoint, 0, len(points))
	for _, p := range points {
		if p.TaskID != 0 || currentCarrier(plan, p.Name, p.Series, p.Family, p.TS) {
			out = append(out, p)
		}
	}
	return out
}

// Epoch maintenance reads only probe-selection fields. Generic storage must
// still permit repairing unrelated malformed settings, and their readers must
// remain responsible for failing closed (for example a retired theme origin).
func probePlanSettings(raw []byte) (ProbeSettings, error) {
	var v struct {
		Enabled             bool                     `json:"enabled"`
		CarrierPing         bool                     `json:"carrier_ping"`
		CarrierProbe        *spec.CarrierProbeConfig `json:"carrier_probe"`
		Carriers            []spec.Carrier           `json:"carriers"`
		CarrierChangedAt    int64                    `json:"carrier_changed_at"`
		MonitoringChangedAt int64                    `json:"monitoring_changed_at"`
	}
	err := json.Unmarshal(raw, &v)
	return ProbeSettings{Enabled: v.Enabled, CarrierPing: v.CarrierPing, CarrierProbe: v.CarrierProbe, Carriers: v.Carriers, CarrierChangedAt: v.CarrierChangedAt, MonitoringChangedAt: v.MonitoringChangedAt}, err
}
