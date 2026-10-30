package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

func TestServiceControlOfflineOptInAndAnchors(t *testing.T) {
	s, id, at := serviceControlRig(t)
	ctx := context.Background()
	policy := ServiceControlPolicy{Action: "limit", ThresholdPercent: 80, LimitMbps: 10, AutoResume: true}
	enabled := true
	legacy := serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy})
	if legacy.OfflineEnabled {
		t.Fatal("default enabled offline policy")
	}
	_, err := s.UpdateNodeServiceControl(ctx, id, ServiceControlChange{Revision: legacy.Revision, OfflineEnabled: &enabled}, at)
	if !errors.Is(err, ErrServiceControl) {
		t.Fatal("old node accepted offline policy", err)
	}
	serviceControlReport(t, s, id, at, &agentproto.ServiceControlReport{Supported: true, OfflineSupported: true, CheckedAt: at.Unix(), Mode: "normal"})
	v := serviceControlPut(t, s, id, at, ServiceControlChange{OfflineEnabled: &enabled})
	d, err := s.DesiredNodeServiceControl(ctx, id)
	if err != nil || !v.OfflineEnabled || d == nil || d.Offline == nil || d.Revision == "" {
		t.Fatal(v, d, err)
	}
	p := d.Offline
	if p.Anchor.UsedUp+p.Anchor.UsedDown != 200 || p.Anchor.Sequence != 2 || p.Anchor.Epoch != "service-control" || len(p.PeriodEnds) != 12 || p.Exempt {
		t.Fatal(p)
	}
	if err = p.Validate(); err != nil {
		t.Fatal(err)
	}
	// Older editors omitting the additive flag preserve the opt-in and identity.
	serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy})
	d2, _ := s.DesiredNodeServiceControl(ctx, id)
	if d2.Offline == nil || d2.Offline.PolicyID != p.PolicyID || d2.Revision != d.Revision {
		t.Fatal("same-value save changed offline policy", d2)
	}
	// Independent NIC observations refresh the authenticated anchor after 2 minutes.
	serviceControlBeat(t, s, id, at.Add(121*time.Second), 3, 220)
	d2, _ = s.DesiredNodeServiceControl(ctx, id)
	if d2.Offline.PolicyID != p.PolicyID || d2.Offline.Anchor.Sequence != 3 || d2.Offline.Anchor.UsedUp+d2.Offline.Anchor.UsedDown != 240 || d2.Revision == d.Revision {
		t.Fatal(d2)
	}
	// A fresh local automatic result can be applied even before panel accounting catches up.
	rep := &agentproto.ServiceControlReport{Supported: true, OfflineSupported: true, CheckedAt: at.Add(121 * time.Second).Unix(), Mode: "limit", LimitMbps: 10, DesiredRevision: d2.Revision, AppliedRevision: d2.Revision, Offline: &agentproto.OfflineControlReport{PolicyID: p.PolicyID, State: "active", Automatic: true, Cycle: p.Cycle, UsedBytes: 800}}
	serviceControlReport(t, s, id, at.Add(121*time.Second), rep)
	v = serviceControlGet(t, s, id, at.Add(121*time.Second))
	if v.Runtime.Status != "applied" || v.Runtime.Offline == nil || v.Automatic.Active {
		t.Fatal(v)
	}
	rep.Offline = nil
	rep.Mode = "normal"
	rep.LimitMbps = 0
	serviceControlReport(t, s, id, at.Add(121*time.Second), rep)
	if v = serviceControlGet(t, s, id, at.Add(121*time.Second)); v.Runtime.Status == "applied" {
		t.Fatal("missing offline result reported applied", v)
	}
	enabled = false
	v = serviceControlPut(t, s, id, at.Add(121*time.Second), ServiceControlChange{OfflineEnabled: &enabled})
	d2, _ = s.DesiredNodeServiceControl(ctx, id)
	if v.OfflineEnabled || d2.Offline != nil {
		t.Fatal("disable did not clear offline snapshot", v, d2)
	}
}

func TestServiceControlOfflineRejectsStaleCapabilityAndPreservesExemption(t *testing.T) {
	s, id, at := serviceControlRig(t)
	ctx := context.Background()
	enabled := true
	policy := ServiceControlPolicy{Action: "pause", ThresholdPercent: 80, AutoResume: true}
	serviceControlReport(t, s, id, at, &agentproto.ServiceControlReport{Supported: true, OfflineSupported: true, CheckedAt: at.Unix(), Mode: "normal"})
	v := serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy, OfflineEnabled: &enabled})
	yes := true
	serviceControlPut(t, s, id, at, ServiceControlChange{ExemptCurrentPeriod: &yes})
	d, _ := s.DesiredNodeServiceControl(ctx, id)
	if !d.Offline.Exempt {
		t.Fatal(d)
	}
	manual := ServiceControlManual{Mode: "pause"}
	serviceControlPut(t, s, id, at, ServiceControlChange{Manual: &manual})
	held, _ := s.DesiredNodeServiceControl(ctx, id)
	if held.Offline.PolicyID != d.Offline.PolicyID || held.Offline.ManualMode != "pause" {
		t.Fatal("manual hold changed automatic identity", held)
	}
	serviceControlPut(t, s, id, at, ServiceControlChange{Policy: &policy})
	next, _ := s.DesiredNodeServiceControl(ctx, id)
	if !next.Offline.Exempt || next.Offline.PolicyID != d.Offline.PolicyID {
		t.Fatal("unchanged save revoked exemption", next)
	}
	v = serviceControlGet(t, s, id, at.Add(10*time.Minute))
	_, err := s.UpdateNodeServiceControl(ctx, id, ServiceControlChange{Revision: v.Revision, OfflineEnabled: &enabled}, at.Add(10*time.Minute))
	if !errors.Is(err, ErrServiceControl) {
		t.Fatal("stale capability enabled policy", err)
	}
	// Explicit disable remains usable when node reports and traffic are stale.
	enabled = false
	serviceControlPut(t, s, id, at.Add(10*time.Minute), ServiceControlChange{OfflineEnabled: &enabled})
}
