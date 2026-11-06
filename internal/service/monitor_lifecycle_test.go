package service

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/testdb"
	"strings"
	"testing"
	"time"
)

func TestMonitorRecoveryAcrossServiceRestartAndMissingMetrics(t *testing.T) {
	ctx := context.Background()
	conn := testdb.Open(t)
	var err error
	st := store.New(conn)
	n := &domain.Node{Name: "<edge>"}
	if err = st.CreateNode(ctx, n, "code", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = st.RedeemPairCode(ctx, "code", "hash", "edge", "v1", "linux"); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	conn.Exec(`UPDATE nodes SET last_seen_at=?`, at.Add(-time.Hour).Unix())
	var settings store.ProbeSettings
	settings.Enabled = true
	settings.Alerts.OfflineSeconds = 30
	settings.Alerts.CPUPct = 80
	st.SetSetting(ctx, store.SettingProbe, settings)
	sent := []string{}
	newProbe := func() *Probe {
		p := &Probe{Store: st, Notify: &notify.Notifier{}, AlertWindow: -1, started: at.Add(-time.Minute)}
		p.adminSend = func(_ context.Context, s string) { sent = append(sent, s) }
		return p
	}
	first := newProbe()
	first.CheckOffline(ctx, at)
	if len(sent) != 1 || !strings.Contains(sent[0], "&lt;edge&gt;") {
		t.Fatalf("open/escaping %q", sent)
	}
	restarted := newProbe()
	restarted.CheckOffline(ctx, at.Add(time.Second))
	if len(sent) != 1 {
		t.Fatal("restart duplicated incident")
	}
	if err = restarted.Record(ctx, n, "", spec.SystemStatus{Valid: &spec.MetricValidity{}}, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || !strings.Contains(sent[1], "recovered") {
		t.Fatalf("restart lost recovery %q", sent)
	}
	if err = restarted.Record(ctx, n, "", spec.SystemStatus{Valid: &spec.MetricValidity{}}, at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatal("duplicate recovery")
	}
	_, err = st.CheckIncident(ctx, n.ID, store.IncidentCheck{Kind: "cpu", Active: true, Value: 95, Threshold: 80}, at.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Record(ctx, n, "", spec.SystemStatus{Valid: &spec.MetricValidity{}}, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	open, _ := st.MonitorIncidents(ctx, n.ID, 0, "open")
	if len(open) != 1 || open[0].Kind != "cpu" {
		t.Fatal("unknown CPU falsely recovered", open)
	}
	settings.Alerts.CPUPct = 0
	st.SetSetting(ctx, store.SettingProbe, settings)
	restarted.Invalidate()
	if err = restarted.Record(ctx, n, "", spec.SystemStatus{Valid: &spec.MetricValidity{}}, at.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	open, _ = st.MonitorIncidents(ctx, n.ID, 0, "open")
	if len(open) != 0 || len(sent) != 2 {
		t.Fatal("disabled rule sent recovery", open, sent)
	}
	ended, _ := st.MonitorIncidents(ctx, n.ID, 0, "resolved")
	for _, i := range ended {
		if i.Kind == "cpu" && (i.Value != 95 || i.Threshold != 80) {
			t.Fatal("disabled rule replaced actual values", i)
		}
	}
}

func TestMonitorPendingBatchHonorsNewSilenceAndDisable(t *testing.T) {
	ctx := context.Background()
	conn := testdb.Open(t)
	var err error
	st := store.New(conn)
	n := &domain.Node{Name: "edge"}
	if err = st.CreateNode(ctx, n, "code", time.Hour); err != nil {
		t.Fatal(err)
	}
	var settings store.ProbeSettings
	settings.Enabled = true
	st.SetSetting(ctx, store.SettingProbe, settings)
	sent := make(chan string, 2)
	p := &Probe{Store: st, Notify: &notify.Notifier{}, AlertWindow: 100 * time.Millisecond}
	p.adminSend = func(_ context.Context, s string) { sent <- s }
	defer p.Reset()
	p.notify(ctx, n, "offline", "queued before silence")
	at := time.Now()
	w := store.MonitorWindow{Kind: "silence", StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix()}
	if err = st.CreateMonitorWindow(ctx, &w, at); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-sent:
		t.Fatal("new silence did not suppress pending notice", s)
	case <-time.After(250 * time.Millisecond):
	}
	if err = st.CancelMonitorWindow(ctx, w.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	p.notify(ctx, n, "offline", "queued before disable")
	if err = p.MonitoringDisabled(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-sent:
		t.Fatal("disable did not discard pending notice", s)
	case <-time.After(250 * time.Millisecond):
	}
}
