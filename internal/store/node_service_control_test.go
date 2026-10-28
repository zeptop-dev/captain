package store

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func serviceControlRig(t *testing.T) (*Store, int64, time.Time) {
	t.Helper()
	s, id := nodeRenewalRig(t)
	at := time.Now().UTC().Truncate(time.Second)
	if err := s.SetSetting(context.Background(), SettingProbe, ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	np, _ := s.NodeProbe(context.Background(), id)
	if err := s.UpdateNodeProbe(context.Background(), id, false, np.Info, 1000, 1, "sum"); err != nil {
		t.Fatal(err)
	}
	serviceControlReport(t, s, id, at, &agentproto.ServiceControlReport{Supported: true, CheckedAt: at.Unix(), Mode: "normal"})
	serviceControlBeat(t, s, id, at.Add(-time.Second), 1, 100)
	serviceControlBeat(t, s, id, at, 2, 200)
	return s, id, at
}
func serviceControlReport(t *testing.T, s *Store, id int64, at time.Time, c *agentproto.ServiceControlReport) {
	t.Helper()
	_, err := s.RecordNodeReport(context.Background(), id, "current-token-hash", agentproto.Report{Version: "test", TrafficEpoch: "service-control", TrafficSeq: 1, ServiceControl: c}, true, at)
	if err != nil {
		t.Fatal(err)
	}
}
func serviceControlBeat(t *testing.T, s *Store, id int64, at time.Time, seq uint64, up uint64) {
	t.Helper()
	h := spec.SystemStatus{Resources: &spec.Resources{Epoch: "service-control", Sequence: seq, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: up, Down: up}}}}
	if err := s.RecordBeat(context.Background(), id, h, at); err != nil {
		t.Fatal(err)
	}
}
func serviceControlGet(t *testing.T, s *Store, id int64, at time.Time) *NodeServiceControl {
	t.Helper()
	v, err := s.NodeServiceControl(context.Background(), id, at)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func serviceControlPut(t *testing.T, s *Store, id int64, at time.Time, c ServiceControlChange) *NodeServiceControl {
	t.Helper()
	c.Revision = serviceControlGet(t, s, id, at).Revision
	v, err := s.UpdateNodeServiceControl(context.Background(), id, c, at)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestServiceControlAutomaticManualAndExemption(t *testing.T) {
	s, id, at := serviceControlRig(t)
	ctx := context.Background()
	initial := serviceControlGet(t, s, id, at)
	if initial.Policy.Action != "alert" || initial.Effective.Mode != "normal" || !initial.Traffic.Fresh || initial.Runtime.Status != "unconfigured" {
		t.Fatal(initial)
	}
	desired, err := s.DesiredNodeServiceControl(ctx, id)
	if err != nil || desired != nil {
		t.Fatal("default changed legacy state", desired, err)
	}
	policy := ServiceControlPolicy{Action: "limit", ThresholdPercent: 80, LimitMbps: 10, AutoResume: true}
	enabled := serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy})
	if enabled.Automatic.Active || enabled.Effective.Revision != "" {
		t.Fatal(enabled)
	}
	serviceControlBeat(t, s, id, at.Add(time.Second), 3, 500) // 800 aggregate bytes reaches 80%.
	triggered := serviceControlGet(t, s, id, at.Add(time.Second))
	if !triggered.Automatic.Active || triggered.Effective.Mode != "limit" || triggered.Effective.LimitMbps != 10 || triggered.Effective.Revision == "" || triggered.Runtime.Status != "pending" {
		t.Fatal(triggered)
	}
	// A valid zero delta preserves command identity rather than re-applying it.
	serviceControlBeat(t, s, id, at.Add(2*time.Second), 4, 500)
	if next := serviceControlGet(t, s, id, at.Add(2*time.Second)); next.Revision != triggered.Revision || next.Effective.Revision != triggered.Effective.Revision {
		t.Fatal("threshold flapped", next)
	}
	manual := ServiceControlManual{Mode: "limit", LimitMbps: 5}
	lower := serviceControlPut(t, s, id, at.Add(2*time.Second), ServiceControlChange{Manual: &manual})
	if lower.Effective.LimitMbps != 5 || !reflect.DeepEqual(lower.Effective.Reasons, []string{"manual", "traffic_quota"}) {
		t.Fatal(lower)
	}
	normal := ServiceControlManual{Mode: "normal"}
	_, err = s.UpdateNodeServiceControl(ctx, id, ServiceControlChange{Revision: lower.Revision, Manual: &normal}, at.Add(2*time.Second))
	if !errors.Is(err, ErrServiceControlExemption) {
		t.Fatal("implicit automatic bypass", err)
	}
	yes := true
	restored := serviceControlPut(t, s, id, at.Add(2*time.Second), ServiceControlChange{Manual: &normal, ExemptCurrentPeriod: &yes})
	if restored.Effective.Mode != "normal" || !restored.Automatic.ExemptCurrentPeriod || restored.Automatic.Active {
		t.Fatal(restored)
	}
	serviceControlBeat(t, s, id, at.Add(3*time.Second), 5, 1500)
	if next := serviceControlGet(t, s, id, at.Add(3*time.Second)); next.Automatic.Active {
		t.Fatal("exemption ignored", next)
	}
	no := false
	revoked := serviceControlPut(t, s, id, at.Add(3*time.Second), ServiceControlChange{ExemptCurrentPeriod: &no})
	if !revoked.Automatic.Active {
		t.Fatal("revocation did not restore protection", revoked)
	}
	policy.Action = "alert"
	policy.LimitMbps = 0
	off := serviceControlPut(t, s, id, at.Add(3*time.Second), ServiceControlChange{Policy: &policy})
	if off.Automatic.Active || off.Effective.Mode != "normal" {
		t.Fatal(off)
	}
	if _, err = s.UpdateNodeServiceControl(ctx, id, ServiceControlChange{Revision: lower.Revision, Manual: &manual}, at); !errors.Is(err, ErrServiceControlConflict) {
		t.Fatal("stale CAS accepted", err)
	}
}
func TestServiceControlRolloverManualResetAndFreshEvidence(t *testing.T) {
	for _, autoResume := range []bool{true, false} {
		t.Run(map[bool]string{true: "resume", false: "hold"}[autoResume], func(t *testing.T) {
			s, id, at := serviceControlRig(t)
			ctx := context.Background()
			policy := ServiceControlPolicy{Action: "pause", ThresholdPercent: 1, AutoResume: autoResume}
			manual := ServiceControlManual{Mode: "pause"}
			v := serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy, Manual: &manual})
			if !v.Automatic.Active {
				t.Fatal(v)
			}
			if err := s.ResetNodeTraffic(ctx, id, at); err != nil {
				t.Fatal(err)
			}
			serviceControlBeat(t, s, id, at.Add(time.Second), 3, 200)
			v = serviceControlGet(t, s, id, at.Add(time.Second))
			if !v.Automatic.Active || v.Manual.Mode != "pause" {
				t.Fatal("manual reset cleared protection", v)
			}
			p, _ := s.NodeProbe(ctx, id)
			next := nodePeriodEnd(at, p)
			// The first report in a new month has no valid NIC observation.
			if err := s.RecordBeat(ctx, id, spec.SystemStatus{Resources: &spec.Resources{Epoch: "service-control", Sequence: 4}}, next); err != nil {
				t.Fatal(err)
			}
			v = serviceControlGet(t, s, id, next)
			if !v.Automatic.Active {
				t.Fatal("unmeasured rollover released restriction", v)
			}
			serviceControlBeat(t, s, id, next.Add(time.Second), 5, 200) // fresh baseline only
			if v = serviceControlGet(t, s, id, next.Add(time.Second)); !v.Automatic.Active {
				t.Fatal("baseline released restriction", v)
			}
			serviceControlBeat(t, s, id, next.Add(2*time.Second), 6, 200) // genuine zero delta
			v = serviceControlGet(t, s, id, next.Add(2*time.Second))
			if v.Automatic.Active == autoResume || v.Manual.Mode != "pause" || v.Effective.Mode != "pause" {
				t.Fatal("incorrect rollover composition", v)
			}
		})
	}
}
func TestServiceControlValidationAndRuntimeEvidence(t *testing.T) {
	s, id, at := serviceControlRig(t)
	ctx := context.Background()
	pause := ServiceControlManual{Mode: "pause"}
	v := serviceControlPut(t, s, id, at, ServiceControlChange{Manual: &pause})
	rep := &agentproto.ServiceControlReport{Supported: true, Mode: "pause", AppliedRevision: v.Effective.Revision, DesiredRevision: v.Effective.Revision, CheckedAt: at.Unix()}
	serviceControlReport(t, s, id, at, rep)
	if got := serviceControlGet(t, s, id, at); got.Runtime.Status != "applied" {
		t.Fatal(got)
	}
	rep.DesiredRevision = "other"
	serviceControlReport(t, s, id, at, rep)
	if got := serviceControlGet(t, s, id, at); got.Runtime.Status == "applied" {
		t.Fatal("mismatched desired reported applied", got)
	}
	rep.DesiredRevision = v.Effective.Revision
	rep.Error = "guard failed"
	serviceControlReport(t, s, id, at, rep)
	if got := serviceControlGet(t, s, id, at); got.Runtime.Status != "error" {
		t.Fatal(got)
	}
	rep.Error = ""
	rep.CheckedAt = at.Add(-4 * time.Minute).Unix()
	serviceControlReport(t, s, id, at, rep)
	if got := serviceControlGet(t, s, id, at); got.Capability.Supported || got.Runtime.Status != "unknown" {
		t.Fatal("fresh envelope revived old check", got)
	}
	normal := ServiceControlManual{Mode: "normal"}
	if got := serviceControlPut(t, s, id, at, ServiceControlChange{Manual: &normal}); got.Effective.Mode != "normal" {
		t.Fatal("offline restoration denied", got)
	}
	p := ServiceControlPolicy{Action: "pause", ThresholdPercent: 100, AutoResume: true}
	if _, err := s.UpdateNodeServiceControl(ctx, id, ServiceControlChange{Revision: serviceControlGet(t, s, id, at).Revision, Policy: &p}, at); !errors.Is(err, ErrServiceControl) {
		t.Fatal("stale capability enabled policy", err)
	}
	if !overServiceControl(ServiceControlTraffic{LimitBytes: math.MaxInt64, UsedBytes: math.MaxInt64}, ServiceControlPolicy{ThresholdPercent: 100}) {
		t.Fatal("large quota overflow")
	}
	if overServiceControl(ServiceControlTraffic{LimitBytes: 101, UsedBytes: 80}, ServiceControlPolicy{ThresholdPercent: 80}) {
		t.Fatal("fractional threshold rounded down")
	}
}
func TestServiceControlMeasurementSettingsQuotaAndIdentity(t *testing.T) {
	s, id, at := serviceControlRig(t)
	ctx := context.Background()
	p := ServiceControlPolicy{Action: "pause", ThresholdPercent: 10, AutoResume: true}
	v := serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &p})
	if !v.Automatic.Active {
		t.Fatal(v)
	}
	np, _ := s.NodeProbe(ctx, id)
	if err := s.UpdateNodeProbe(ctx, id, false, np.Info, 0, np.ResetDay, np.Mode); !errors.Is(err, ErrServiceControl) {
		t.Fatal("removed protected quota", err)
	}
	if err := s.UpdateNodeProbe(ctx, id, false, np.Info, 10000, np.ResetDay, np.Mode); err != nil {
		t.Fatal(err)
	}
	if got := serviceControlGet(t, s, id, at); got.Automatic.Active {
		t.Fatal("quota raise did not release hold", got)
	}
	resources := spec.ResourceOptions{IncludeInterfaces: []string{"eth1"}}
	if err := s.UpdateNodeProbe(ctx, id, false, np.Info, 10000, np.ResetDay, np.Mode, &resources); err != nil {
		t.Fatal(err)
	}
	if got := serviceControlGet(t, s, id, at); got.Traffic.Fresh {
		t.Fatal("basis change retained old fresh sample", got)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	n := &domain.Node{Name: "replacement"}
	if err := s.CreateNode(ctx, n, "replacement", time.Hour); err != nil {
		t.Fatal(err)
	}
	got := serviceControlGet(t, s, n.ID, at)
	if got.Revision == v.Revision || got.Policy.Action != "alert" || got.Effective.Mode != "normal" {
		t.Fatal("replacement inherited control", got)
	}
}
