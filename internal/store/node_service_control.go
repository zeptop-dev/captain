package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

var (
	ErrServiceControl          = errors.New("invalid server service control")
	ErrServiceControlConflict  = errors.New("service control changed; reload before saving")
	ErrServiceControlExemption = errors.New("automatic traffic protection is active; explicitly exempt this period before restoring service")
)

const serviceControlFresh = 3 * time.Minute

type ServiceControlPolicy struct {
	Action           string `json:"action"`
	ThresholdPercent int    `json:"threshold_percent"`
	LimitMbps        int    `json:"limit_mbps"`
	AutoResume       bool   `json:"auto_resume"`
}
type ServiceControlManual struct {
	Mode      string `json:"mode"`
	LimitMbps int    `json:"limit_mbps"`
}
type ServiceControlAutomatic struct {
	Active              bool       `json:"active"`
	Mode                string     `json:"mode"`
	LimitMbps           int        `json:"limit_mbps"`
	TriggeredAt         *time.Time `json:"triggered_at,omitempty"`
	PeriodStart         *time.Time `json:"period_start,omitempty"`
	ExemptCurrentPeriod bool       `json:"exempt_current_period"`
}
type ServiceControlEffective struct {
	Mode      string   `json:"mode"`
	LimitMbps int      `json:"limit_mbps"`
	Revision  string   `json:"revision"`
	Reasons   []string `json:"reasons"`
}
type ServiceControlTraffic struct {
	MonitoringEnabled bool       `json:"monitoring_enabled"`
	LimitBytes        int64      `json:"limit_bytes"`
	UsedBytes         int64      `json:"used_bytes"`
	PeriodStart       *time.Time `json:"period_start,omitempty"`
	PeriodEnd         *time.Time `json:"period_end,omitempty"`
	ObservedAt        *time.Time `json:"observed_at,omitempty"`
	Fresh             bool       `json:"fresh"`
}
type ServiceControlCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}
type ServiceControlRuntime struct {
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Mode       string     `json:"mode,omitempty"`
	LimitMbps  int        `json:"limit_mbps,omitempty"`
	Error      string     `json:"error,omitempty"`
}
type NodeServiceControl struct {
	NodeID     int64                    `json:"node_id"`
	Revision   string                   `json:"revision"`
	Policy     ServiceControlPolicy     `json:"policy"`
	Manual     ServiceControlManual     `json:"manual"`
	Automatic  ServiceControlAutomatic  `json:"automatic"`
	Effective  ServiceControlEffective  `json:"effective"`
	Traffic    ServiceControlTraffic    `json:"traffic"`
	Capability ServiceControlCapability `json:"capability"`
	Runtime    ServiceControlRuntime    `json:"runtime"`
}
type ServiceControlChange struct {
	Revision            string                `json:"revision"`
	Policy              *ServiceControlPolicy `json:"policy,omitempty"`
	Manual              *ServiceControlManual `json:"manual,omitempty"`
	ExemptCurrentPeriod *bool                 `json:"exempt_current_period,omitempty"`
}

// Cycles advance only on a scheduled counter rollover. A manual reset or an
// edited reset day cannot impersonate a paid new period and clear protection.
type serviceControlState struct {
	Policy          ServiceControlPolicy    `json:"policy"`
	Manual          ServiceControlManual    `json:"manual"`
	Automatic       ServiceControlAutomatic `json:"automatic"`
	Cycle           int64                   `json:"cycle"`
	ExemptCycle     int64                   `json:"exempt_cycle"`
	PendingRollover bool                    `json:"pending_rollover"`
}
type serviceControlRecord struct {
	revision, command string
	state             serviceControlState
}

func defaultServiceControl() serviceControlState {
	return serviceControlState{Policy: ServiceControlPolicy{Action: "alert", ThresholdPercent: 100, AutoResume: true}, Manual: ServiceControlManual{Mode: "normal"}, Automatic: ServiceControlAutomatic{Mode: "normal"}, Cycle: 1}
}
func serviceControlTx(ctx context.Context, tx *sql.Tx, id int64) (serviceControlRecord, error) {
	r := serviceControlRecord{state: defaultServiceControl()}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT revision,command_revision,state_json FROM node_service_control WHERE node_id=?`, id).Scan(&r.revision, &r.command, &raw)
	if err != nil {
		return r, wrapNotFound(err)
	}
	if err = json.Unmarshal([]byte(raw), &r.state); err != nil {
		return r, err
	}
	return r, nil
}
func saveServiceControlTx(ctx context.Context, tx *sql.Tx, id int64, r *serviceControlRecord, old serviceControlRecord) error {
	if reflect.DeepEqual(r.state, old.state) {
		return nil
	}
	r.revision = rand.Text()
	before, after := old.effective(), r.effective()
	if before.Mode != after.Mode || before.LimitMbps != after.LimitMbps {
		r.command = rand.Text()
	}
	raw, err := json.Marshal(r.state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_service_control SET revision=?,command_revision=?,state_json=? WHERE node_id=?`, r.revision, r.command, string(raw), id)
	return err
}
func (r serviceControlRecord) effective() ServiceControlEffective {
	v := ServiceControlEffective{Mode: "normal", Revision: r.command, Reasons: []string{}}
	add := func(mode string, limit int, reason string) {
		if mode == "normal" {
			return
		}
		v.Reasons = append(v.Reasons, reason)
		if mode == "pause" {
			v.Mode = "pause"
			v.LimitMbps = 0
			return
		}
		if v.Mode != "pause" && (v.Mode == "normal" || limit < v.LimitMbps) {
			v.Mode = "limit"
			v.LimitMbps = limit
		}
	}
	add(r.state.Manual.Mode, r.state.Manual.LimitMbps, "manual")
	if r.state.Automatic.Active {
		add(r.state.Automatic.Mode, r.state.Automatic.LimitMbps, "traffic_quota")
	}
	return v
}
func serviceControlTrafficTx(ctx context.Context, tx *sql.Tx, p *NodeProbe, at time.Time) (ServiceControlTraffic, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbe).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ServiceControlTraffic{}, err
	}
	var settings ProbeSettings
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &settings); err != nil {
			return ServiceControlTraffic{}, err
		}
	}
	v := ServiceControlTraffic{MonitoringEnabled: settings.Enabled, LimitBytes: p.LimitBytes, UsedBytes: p.Billed()}
	if !p.PeriodStart.IsZero() {
		t := p.PeriodStart
		v.PeriodStart = &t
		end := nodePeriodEnd(nodePeriodStart(at, p), p)
		v.PeriodEnd = &end
	}
	if p.counters.TrafficKnown && p.counters.TrafficAt > 0 {
		t := time.Unix(p.counters.TrafficAt, 0)
		v.ObservedAt = &t
		basisMatches := true
		for _, n := range p.counters.Networks {
			if n.Included != p.Resources.Includes(n.Name) {
				basisMatches = false
				break
			}
		}
		v.Fresh = basisMatches && settings.Enabled && !t.After(at.Add(5*time.Second)) && at.Sub(t) <= serviceControlFresh
	}
	return v, nil
}
func serviceControlCapabilityTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (ServiceControlCapability, ServiceControlRuntime, *agentproto.ServiceControlReport, error) {
	var mode, raw string
	var token sql.NullString
	var observed sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT node_mode,token_hash,runtime_report_json,runtime_reported_at FROM nodes WHERE id=?`, id).Scan(&mode, &token, &raw, &observed)
	cap := ServiceControlCapability{}
	runtime := ServiceControlRuntime{Status: "unsupported"}
	if err != nil {
		return cap, runtime, nil, wrapNotFound(err)
	}
	if mode == string(spec.NodeMonitor) {
		cap.Reason = "monitor_only"
		return cap, runtime, nil, nil
	}
	if !token.Valid || token.String == "" {
		cap.Reason = "unpaired"
		return cap, runtime, nil, nil
	}
	if !observed.Valid || observed.Int64 <= 0 {
		cap.Reason = "upgrade_required"
		return cap, runtime, nil, nil
	}
	t := time.Unix(observed.Int64, 0)
	runtime.ObservedAt = &t
	if t.After(at.Add(5*time.Second)) || at.Sub(t) > serviceControlFresh {
		cap.Reason = "report_stale"
		runtime.Status = "unknown"
		return cap, runtime, nil, nil
	}
	var report NodeRuntime
	if err = json.Unmarshal([]byte(raw), &report); err != nil {
		return cap, runtime, nil, err
	}
	rep := report.ServiceControl
	if rep == nil {
		cap.Reason = "upgrade_required"
		return cap, runtime, nil, nil
	}
	checked := time.Unix(rep.CheckedAt, 0)
	if rep.CheckedAt <= 0 || checked.After(at.Add(5*time.Second)) || at.Sub(checked) > serviceControlFresh {
		cap.Reason = "report_stale"
		runtime.Status = "unknown"
		return cap, runtime, nil, nil
	}
	cap.Supported = rep.Supported
	if !cap.Supported {
		cap.Reason = "unavailable"
		if rep.Reason == "unsupported_platform" {
			cap.Reason = rep.Reason
		}
	}
	runtime.Mode, runtime.LimitMbps, runtime.Error = rep.Mode, rep.LimitMbps, rep.Error
	return cap, runtime, rep, nil
}
func nodeServiceControlTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (*NodeServiceControl, error) {
	r, err := serviceControlTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	traffic, err := serviceControlTrafficTx(ctx, tx, p, at)
	if err != nil {
		return nil, err
	}
	capability, runtime, rep, err := serviceControlCapabilityTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	effective := r.effective()
	if capability.Supported {
		switch {
		case r.command == "":
			runtime.Status = "unconfigured"
		case rep.Error != "":
			runtime.Status = "error"
		case rep.DesiredRevision == r.command && rep.AppliedRevision == r.command && rep.Mode == effective.Mode && rep.LimitMbps == effective.LimitMbps:
			runtime.Status = "applied"
		default:
			runtime.Status = "pending"
		}
	}
	automatic := r.state.Automatic
	automatic.ExemptCurrentPeriod = r.state.ExemptCycle == r.state.Cycle
	return &NodeServiceControl{NodeID: id, Revision: r.revision, Policy: r.state.Policy, Manual: r.state.Manual, Automatic: automatic, Effective: effective, Traffic: traffic, Capability: capability, Runtime: runtime}, nil
}
func (s *Store) NodeServiceControl(ctx context.Context, id int64, at time.Time) (*NodeServiceControl, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	v, err := nodeServiceControlTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	return v, tx.Commit()
}
func validateServiceControlPolicy(p ServiceControlPolicy) error {
	if p.Action != "alert" && p.Action != "pause" && p.Action != "limit" {
		return fmt.Errorf("%w: unknown automatic action", ErrServiceControl)
	}
	if p.ThresholdPercent < 1 || p.ThresholdPercent > 100 {
		return fmt.Errorf("%w: threshold must be between 1 and 100 percent", ErrServiceControl)
	}
	return validateServiceControlRate(p.Action, p.LimitMbps)
}
func validateServiceControlRate(mode string, rate int) error {
	if rate < 0 || rate > 1000000 || (mode == "limit" && rate == 0) || (mode != "limit" && rate != 0) {
		return fmt.Errorf("%w: limit requires 1–1000000 Mbps; other modes require zero", ErrServiceControl)
	}
	return nil
}
func clearAutomatic(s *serviceControlState) { s.Automatic = ServiceControlAutomatic{Mode: "normal"} }
func overServiceControl(usage ServiceControlTraffic, p ServiceControlPolicy) bool {
	// Integer quotient/remainder avoids overflowing large valid byte counters.
	threshold := usage.LimitBytes/100*int64(p.ThresholdPercent) + (usage.LimitBytes%100*int64(p.ThresholdPercent)+99)/100
	return usage.LimitBytes > 0 && usage.UsedBytes >= threshold
}
func evaluateServiceControl(r *serviceControlRecord, usage ServiceControlTraffic, at time.Time, explicit bool) {
	s := &r.state
	if s.Policy.Action == "alert" {
		clearAutomatic(s)
		return
	}
	if !usage.Fresh || usage.LimitBytes <= 0 {
		return
	}
	if s.PendingRollover {
		if s.Policy.AutoResume {
			clearAutomatic(s)
		}
		s.PendingRollover = false
	}
	if s.ExemptCycle == s.Cycle {
		clearAutomatic(s)
		return
	}
	if overServiceControl(usage, s.Policy) {
		if !s.Automatic.Active {
			t := at
			s.Automatic.TriggeredAt = &t
			s.Automatic.PeriodStart = usage.PeriodStart
		}
		s.Automatic.Active = true
		s.Automatic.Mode = s.Policy.Action
		s.Automatic.LimitMbps = s.Policy.LimitMbps
	} else if explicit {
		clearAutomatic(s)
	}
}
func (s *Store) UpdateNodeServiceControl(ctx context.Context, id int64, c ServiceControlChange, at time.Time) (*NodeServiceControl, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := serviceControlTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	old := r
	if c.Revision == "" || c.Revision != r.revision {
		return nil, ErrServiceControlConflict
	}
	if c.Policy == nil && c.Manual == nil && c.ExemptCurrentPeriod == nil {
		return nil, fmt.Errorf("%w: no changes provided", ErrServiceControl)
	}
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	usage, err := serviceControlTrafficTx(ctx, tx, p, at)
	if err != nil {
		return nil, err
	}
	cap, _, _, err := serviceControlCapabilityTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	if c.Policy != nil {
		if err = validateServiceControlPolicy(*c.Policy); err != nil {
			return nil, err
		}
		if c.Policy.Action != "alert" {
			if !cap.Supported {
				return nil, fmt.Errorf("%w: server does not currently support service control (%s)", ErrServiceControl, cap.Reason)
			}
			if !usage.MonitoringEnabled || usage.LimitBytes <= 0 || !usage.Fresh {
				return nil, fmt.Errorf("%w: automatic protection requires enabled monitoring, a positive quota and a fresh measured traffic sample", ErrServiceControl)
			}
		}
		r.state.Policy = *c.Policy
		r.state.ExemptCycle = 0
		evaluateServiceControl(&r, usage, at, true)
	}
	if c.ExemptCurrentPeriod != nil {
		if *c.ExemptCurrentPeriod {
			if usage.PeriodStart == nil {
				return nil, fmt.Errorf("%w: no traffic period is available", ErrServiceControl)
			}
			r.state.ExemptCycle = r.state.Cycle
			clearAutomatic(&r.state)
		} else {
			r.state.ExemptCycle = 0
			evaluateServiceControl(&r, usage, at, false)
		}
	}
	if c.Manual != nil {
		if c.Manual.Mode != "normal" && c.Manual.Mode != "pause" && c.Manual.Mode != "limit" {
			return nil, fmt.Errorf("%w: unknown manual mode", ErrServiceControl)
		}
		if err = validateServiceControlRate(c.Manual.Mode, c.Manual.LimitMbps); err != nil {
			return nil, err
		}
		if c.Manual.Mode != "normal" && !cap.Supported {
			return nil, fmt.Errorf("%w: server does not currently support service control (%s)", ErrServiceControl, cap.Reason)
		}
		if c.Manual.Mode == "normal" && r.state.Automatic.Active {
			return nil, ErrServiceControlExemption
		}
		r.state.Manual = *c.Manual
	}
	if err = saveServiceControlTx(ctx, tx, id, &r, old); err != nil {
		return nil, err
	}
	out, err := nodeServiceControlTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Called in the same transaction as accepted NIC deltas, including a rollover
// whose first report had no usable counters. Only a later valid delta releases it.
func recordServiceControlTrafficTx(ctx context.Context, tx *sql.Tx, id int64, p *NodeProbe, at time.Time, measured, rolled bool) error {
	r, err := serviceControlTx(ctx, tx, id)
	if err != nil {
		return err
	}
	old := r
	if rolled {
		r.state.Cycle++
		r.state.PendingRollover = true
	}
	if measured {
		usage, err := serviceControlTrafficTx(ctx, tx, p, at)
		if err != nil {
			return err
		}
		evaluateServiceControl(&r, usage, at, false)
	}
	return saveServiceControlTx(ctx, tx, id, &r, old)
}

// DesiredNodeServiceControl is private state. A prior command is retained while
// a node is offline/unsupported so it cannot silently restart without the guard.
func (s *Store) DesiredNodeServiceControl(ctx context.Context, id int64) (*spec.ServiceControl, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := serviceControlTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if r.command == "" {
		return nil, nil
	}
	v := r.effective()
	return &spec.ServiceControl{Revision: v.Revision, Mode: v.Mode, LimitMbps: v.LimitMbps}, nil
}

func (s *Store) HasAutomaticServiceControl(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_service_control WHERE COALESCE(json_extract(state_json,'$.policy.action'),'alert') <> 'alert'`).Scan(&count)
	return count > 0, err
}
