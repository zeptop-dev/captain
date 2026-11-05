package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func probePlanRig(t *testing.T) (*Store, int64) {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "Monitor", Mode: spec.NodeMonitor}
	if err = s.CreateNode(context.Background(), n, "pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	return s, n.ID
}
func TestNodeProbePlanLifecycleAndIsolation(t *testing.T) {
	s, id := probePlanRig(t)
	ctx := context.Background()
	global := ProbeSettings{Enabled: true, CarrierPing: true, CarrierProbe: &spec.CarrierProbeConfig{Protocol: "tcp", Family: "ipv4"}}
	if err := s.SetSetting(ctx, SettingProbe, global); err != nil {
		t.Fatal(err)
	}
	get := func() NodeProbePlanView {
		t.Helper()
		p, err := s.NodeProbePlanView(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := get()
	if p.Mode != "inherit" || p.Custom != nil || !p.Effective.Enabled || p.Revision == "" || p.Effective.ChangedAt != 0 {
		t.Fatal(p)
	}
	at := time.Now().Add(-time.Hour)
	save := func(mode string, cfg *spec.CarrierProbeConfig) NodeProbePlanView {
		t.Helper()
		v, err := s.UpdateNodeProbePlan(ctx, id, NodeProbePlanChange{Revision: p.Revision, Mode: mode, Custom: cfg}, at)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	original := p.Revision
	p = save("custom", nil)
	if p.Custom == nil || p.Custom.Protocol != "tcp" || p.Effective.ChangedAt != 0 {
		t.Fatal("first custom did not clone without changing measurement", p)
	}
	if _, err := s.UpdateNodeProbePlan(ctx, id, NodeProbePlanChange{Revision: original, Mode: "disabled"}, at); !errors.Is(err, ErrProbePlanConflict) {
		t.Fatal("stale CAS", err)
	}
	p = save("custom", &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"})
	if p.Effective.Config.Protocol != "icmp" || p.Effective.ChangedAt != at.Unix() {
		t.Fatal(p)
	}
	changed := p.Effective.ChangedAt
	before, _ := s.ProbePublicPolicyFingerprint(ctx)
	global.Title = "Appearance"
	global.CarrierProbe = &spec.CarrierProbeConfig{Protocol: "tcp", Family: "dual"}
	if err := s.SetSetting(ctx, SettingProbe, global); err != nil {
		t.Fatal(err)
	}
	if v := get(); v.Effective.ChangedAt != changed || v.Effective.Config.Protocol != "icmp" {
		t.Fatal("global carrier changed custom", v)
	}
	p = save("inherit", nil)
	if p.Custom == nil || p.Effective.Config.Protocol != "tcp" || p.Effective.Config.Family != "dual" {
		t.Fatal("draft not retained", p)
	}
	p = save("custom", nil)
	if p.Effective.Config.Protocol != "icmp" {
		t.Fatal(p)
	}
	p = save("disabled", nil)
	if p.Effective.Enabled || p.Custom == nil {
		t.Fatal(p)
	}
	after, _ := s.ProbePublicPolicyFingerprint(ctx)
	if before == after {
		t.Fatal("policy fingerprint omitted plan")
	}
	// Broad old editors update metadata only, leaving the independent plan intact.
	if err := s.UpdateNodeProbe(ctx, id, false, NodeProbeInfo{}, 0, 1, "sum"); err != nil {
		t.Fatal(err)
	}
	if v := get(); v.Revision != p.Revision || v.Mode != "disabled" {
		t.Fatal(v)
	}
	global.CarrierPing = false
	if err := s.SetSetting(ctx, SettingProbe, global); err != nil {
		t.Fatal(err)
	}
	p = save("custom", nil)
	if !p.Effective.Enabled {
		t.Fatal("default off suppressed custom")
	}
	global.Enabled = false
	if err := s.SetSetting(ctx, SettingProbe, global); err != nil {
		t.Fatal(err)
	}
	if p = get(); p.Effective.Enabled || p.Effective.ChangedAt <= changed {
		t.Fatal("master gate did not expire custom samples", p)
	}
	// Node deletion/recreation cannot accept an old draft, even with the same ID.
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,'replacement',0,0)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateNodeProbePlan(ctx, id, NodeProbePlanChange{Revision: p.Revision, Mode: "custom"}, at); !errors.Is(err, ErrProbePlanConflict) {
		t.Fatal("old incarnation accepted", err)
	}
}
func TestProbePlanEpochsAndCurrentSamples(t *testing.T) {
	s, id := probePlanRig(t)
	ctx := context.Background()
	g := ProbeSettings{Enabled: true, CarrierPing: true, CarrierProbe: &spec.CarrierProbeConfig{Protocol: "tcp", Family: "ipv4"}, CarrierChangedAt: 9999999999}
	if err := s.SetSetting(ctx, SettingProbe, g); err != nil {
		t.Fatal(err)
	}
	g = ProbeSettings{}
	_ = s.GetSetting(ctx, SettingProbe, &g)
	if g.CarrierChangedAt != 0 {
		t.Fatal("client epoch trusted")
	}
	p, _ := s.NodeProbePlanView(ctx, id)
	a := p.Effective.Config.Tasks()[0]
	sample := spec.PingResult{TaskID: 0, Name: a.Name, Series: a.Series, Family: a.Family, At: time.Now().Add(-time.Minute).Unix()}
	host := spec.SystemStatus{CarrierProbeVersion: 1, Pings: []spec.PingResult{sample, {TaskID: 7, Name: "Independent"}}}
	if got := FilterCarrierSamples(host, p.Effective); len(got.Pings) != 2 {
		t.Fatal(got)
	}
	g.CarrierProbe = &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"}
	_ = s.SetSetting(ctx, SettingProbe, g)
	g.CarrierProbe = &spec.CarrierProbeConfig{Protocol: "tcp", Family: "ipv4"}
	_ = s.SetSetting(ctx, SettingProbe, g)
	p, _ = s.NodeProbePlanView(ctx, id)
	if got := FilterCarrierSamples(host, p.Effective); len(got.Pings) != 1 || got.Pings[0].TaskID != 7 {
		t.Fatal("A-B-A revived old data", got)
	}
	sample.At = p.Effective.ChangedAt + 1
	host.Pings[0] = sample
	if got := FilterCarrierSamples(host, p.Effective); len(got.Pings) != 2 {
		t.Fatal("fresh measurement missing", got)
	}
	host.CarrierProbeVersion = 0
	if got := FilterCarrierSamples(host, p.Effective); len(got.Pings) != 1 {
		t.Fatal("legacy report accepted", got)
	}
	points := []PingPoint{{Name: a.Name, Series: a.Series, Family: a.Family, TS: p.Effective.ChangedAt}, {Name: a.Name, Series: a.Series, Family: a.Family, TS: p.Effective.ChangedAt + 1}, {TaskID: 7}}
	if got := FilterCarrierPoints(points, p.Effective); len(got) != 3 || got[0].TS != p.Effective.ChangedAt {
		t.Fatal("same-target history must remain timestamped and accessible", got)
	}
	// An unrelated setting or inactive list edit must not clear the active results.
	_ = s.GetSetting(ctx, SettingProbe, &g)
	epoch := g.CarrierChangedAt
	g.Title = "Another title"
	g.CarrierProbe.ICMPIPv4 = []spec.Carrier{{Name: "Inactive", Addr: "192.0.2.10"}}
	_ = s.SetSetting(ctx, SettingProbe, g)
	_ = s.GetSetting(ctx, SettingProbe, &g)
	if g.CarrierChangedAt != epoch {
		t.Fatal("inactive draft cleared live results")
	}
	for _, in := range []NodeProbePlanChange{{Mode: "bogus"}, {Mode: "custom", Custom: &spec.CarrierProbeConfig{Protocol: "udp"}}} {
		in.Revision = p.Revision
		if _, err := s.UpdateNodeProbePlan(ctx, id, in, time.Now()); !errors.Is(err, ErrProbePlan) {
			t.Fatal(err)
		}
	}
}

func TestProbePlanRollingWindowBoundary(t *testing.T) {
	cfg := &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"}
	task := cfg.Tasks()[0]
	plan := EffectiveProbePlan{Enabled: true, Config: cfg, ChangedAt: 2000}
	for _, start := range []int64{0, 1999, 2000, 2001, 2011} {
		original := spec.PingResult{Name: task.Name, Family: task.Family, Series: task.Series, At: 2010, LatencyMs: 24, Loss: 50, Quality: &spec.PingQuality{WindowStartedAt: start, Window: spec.ProbeWindow{Attempts: 30, Failed: 15}}}
		host := spec.SystemStatus{CarrierProbeVersion: 1, Pings: []spec.PingResult{original}}
		got := FilterCarrierSamples(host, plan).Pings[0]
		if got.LatencyMs != 24 || got.At != 2010 {
			t.Fatal("fresh latest sample lost", got)
		}
		valid := start > 2000 && start <= 2010
		if valid && (got.Quality.Window.Attempts != 30 || got.Loss != 50) {
			t.Fatal("valid window lost", got)
		}
		if !valid && (got.Quality.Window.Attempts != 0 || got.Loss != 0) {
			t.Fatal("old window retained", got)
		}
		if original.Quality.Window.Attempts != 30 {
			t.Fatal("shared host mutated")
		}
	}
	at := time.Unix(2000, 0)
	h := spec.SystemStatus{Resources: &spec.Resources{At: 1000000}, Pings: []spec.PingResult{{At: 995, Quality: &spec.PingQuality{WindowStartedAt: 900}}}}
	got := MonitorSnapshot(h, at).Pings[0]
	if got.At != 1995 || got.Quality.WindowStartedAt != 1900 || h.Pings[0].Quality.WindowStartedAt != 900 {
		t.Fatal("clock normalization", got)
	}
	for _, start := range []int64{0, -1, 996} {
		h.Pings[0].Quality.WindowStartedAt = start
		if got := MonitorSnapshot(h, at).Pings[0]; got.Quality.WindowStartedAt != 0 {
			t.Fatal("invalid boundary became fresh", got)
		}
	}
	h.Resources.At = 100000000
	h.Pings[0].At = 99995
	h.Pings[0].Quality.WindowStartedAt = 100
	if got := MonitorSnapshot(h, at).Pings[0]; got.Quality.WindowStartedAt != 0 {
		t.Fatal("unbounded age became fresh", got)
	}
}

func TestProbePlanEpochsPreserveMalformedSettingRepair(t *testing.T) {
	s, id := probePlanRig(t)
	ctx := context.Background()
	for _, bad := range []any{map[string]any{"enabled": true, "hosts": "invalid"}, map[string]any{"enabled": true, "carrier_probe": "invalid"}, "invalid document", []any{"invalid document"}} {
		if err := s.SetSetting(ctx, SettingProbe, bad); err != nil {
			t.Fatal("generic store blocked malformed document fixture", err)
		}
		var raw ProbeSettings
		if err := s.GetSetting(ctx, SettingProbe, &raw); err == nil {
			t.Fatal("malformed setting silently accepted by typed reader")
		}
		if err := s.SetSetting(ctx, SettingProbe, ProbeSettings{Enabled: true, CarrierPing: true}); err != nil {
			t.Fatal("cannot repair invalid settings", err)
		}
		v, err := s.NodeProbePlanView(ctx, id)
		if err != nil || !v.Effective.Enabled {
			t.Fatal("repair did not restore valid effective plan", v, err)
		}
	}
}
