package store

import (
	"context"
	"github.com/zeptop-dev/captain/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func monitorRig(t *testing.T, mode ...fixtureDBMode) (*Store, int64, time.Time) {
	t.Helper()
	conn := fixtureDB(t, filepath.Join(t.TempDir(), "monitor.db"), mode...)
	var err error
	s := New(conn)
	n := &domain.Node{Name: "edge"}
	if err = s.CreateNode(context.Background(), n, "monitor", time.Hour); err != nil {
		t.Fatal(err)
	}
	return s, n.ID, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
}

func TestMonitorIncidentSilenceRecoveryAndAcknowledgement(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	w := MonitorWindow{Kind: "silence", StartsAt: at.Unix(), EndsAt: at.Add(time.Minute).Unix()}
	if err := s.CreateMonitorWindow(ctx, &w, at); err != nil {
		t.Fatal(err)
	}
	c := IncidentCheck{Kind: "cpu", Active: true, Value: 95, Threshold: 80}
	n, err := s.CheckIncident(ctx, id, c, at)
	if err != nil || n != nil {
		t.Fatalf("silenced open: %+v %v", n, err)
	}
	rows, _ := s.MonitorIncidents(ctx, id, 0, "open")
	if len(rows) != 1 || rows[0].NotifiedAt != 0 {
		t.Fatal(rows)
	}
	incidentID := rows[0].ID
	if err = s.AcknowledgeIncident(ctx, incidentID, "operator@example.com", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Persisted state survives a new service/store instance and notifies once on expiry.
	restarted := New(s.DB())
	n, err = restarted.CheckIncident(ctx, id, c, at.Add(time.Minute))
	if err != nil || n == nil || n.ID != incidentID || n.Recovered {
		t.Fatalf("expiry: %+v %v", n, err)
	}
	if n, err = restarted.CheckIncident(ctx, id, c, at.Add(2*time.Minute)); err != nil || n != nil {
		t.Fatalf("repeat %+v %v", n, err)
	}
	c.Active = false
	c.Value = 20
	n, err = restarted.CheckIncident(ctx, id, c, at.Add(3*time.Minute))
	if err != nil || n == nil || !n.Recovered {
		t.Fatalf("recovery %+v %v", n, err)
	}
	if n, _ = s.CheckIncident(ctx, id, c, at.Add(4*time.Minute)); n != nil {
		t.Fatal("duplicate recovery")
	}
	rows, _ = s.MonitorIncidents(ctx, id, 0, "resolved")
	if len(rows) != 1 || rows[0].AcknowledgedBy != "operator@example.com" || rows[0].EndedAt == nil {
		t.Fatal(rows)
	}
	c.Active = true
	n, err = s.CheckIncident(ctx, id, c, at.Add(5*time.Minute))
	if err != nil || n == nil || n.ID == incidentID {
		t.Fatalf("new occurrence %+v %v", n, err)
	}
	c.Active = false
	c.Resolution = "disabled"
	if n, err = s.CheckIncident(ctx, id, c, at.Add(6*time.Minute)); err != nil || n != nil {
		t.Fatalf("disabled not recovered %+v %v", n, err)
	}
}
func TestMonitorIncidentEntirelyMutedAndStorageRollback(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	w := MonitorWindow{NodeID: &id, Kind: "maintenance", StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix()}
	if err := s.CreateMonitorWindow(ctx, &w, at); err != nil {
		t.Fatal(err)
	}
	for i, active := range []bool{true, false} {
		n, err := s.CheckIncident(ctx, id, IncidentCheck{Kind: "offline", Active: active}, at.Add(time.Duration(i)*time.Minute))
		if err != nil || n != nil {
			t.Fatalf("muted lifecycle %+v %v", n, err)
		}
	}
	if err := s.CancelMonitorWindow(ctx, w.ID, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`CREATE TRIGGER fail_incident_notify BEFORE UPDATE OF notified_at ON monitor_incidents BEGIN SELECT RAISE(FAIL, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	n, err := s.CheckIncident(ctx, id, IncidentCheck{Kind: "offline", Active: true}, at.Add(3*time.Minute))
	if err == nil || n != nil {
		t.Fatal("claimed failed write")
	}
	rows, _ := s.MonitorIncidents(ctx, id, 0, "open")
	if len(rows) != 0 {
		t.Fatal("partial transaction", rows)
	}
	s.DB().Exec(`DROP TRIGGER fail_incident_notify`)
	n, err = s.CheckIncident(ctx, id, IncidentCheck{Kind: "offline", Active: true}, at.Add(3*time.Minute))
	if err != nil || n == nil {
		t.Fatalf("retry %+v %v", n, err)
	}
}
func TestMonitorAvailabilityUnknownRestartAndMaintenanceUnion(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	observe := func(epoch string, second, last int64) {
		t.Helper()
		if err := s.ObserveAvailability(ctx, id, epoch, at.Add(time.Duration(second)*time.Second), at.Add(time.Duration(last)*time.Second), 30*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	observe("one", 0, 0)
	observe("one", 60, 0)    // 30 online, 30 offline
	observe("one", 120, 120) // 60 offline; new heartbeat arrives at interval end
	observe("two", 180, 180) // restart: 60 unknown, even with a recent persisted observation
	observe("two", 240, 240) // 30 online, 30 offline before the next heartbeat
	observe("two", 600, 600) // scheduler stall: 360 unknown
	observe("two", 660, 660) // 30 online, 30 offline
	a, err := s.Availability(ctx, id, at, at.Add(660*time.Second))
	if err != nil || a.Online != 90 || a.Offline != 150 || a.Unknown != 420 || a.Maintenance != 0 {
		t.Fatalf("availability %+v %v", a, err)
	}
	// Overlapping global/node maintenance subtracts a union. Silence does not.
	for _, w := range []MonitorWindow{{Kind: "maintenance", StartsAt: at.Add(20 * time.Second).Unix(), EndsAt: at.Add(50 * time.Second).Unix()}, {NodeID: &id, Kind: "maintenance", StartsAt: at.Add(40 * time.Second).Unix(), EndsAt: at.Add(80 * time.Second).Unix()}, {Kind: "silence", StartsAt: at.Add(80 * time.Second).Unix(), EndsAt: at.Add(100 * time.Second).Unix()}} {
		if err = s.CreateMonitorWindow(ctx, &w, at); err != nil {
			t.Fatal(err)
		}
	}
	a, err = s.Availability(ctx, id, at, at.Add(660*time.Second))
	if err != nil || a.Online != 80 || a.Offline != 100 || a.Unknown != 420 || a.Maintenance != 60 {
		t.Fatalf("maintenance union %+v %v", a, err)
	}
	if a.Percent == nil || *a.Percent < 44.4 || *a.Percent > 44.5 || a.Coverage == nil || *a.Coverage != 30 {
		t.Fatalf("denominators %+v", a)
	}
	var total int64
	for _, b := range a.Buckets {
		total += b.Online + b.Offline + b.Unknown + b.Maintenance
	}
	if total != 660 {
		t.Fatal("bucket sum", total)
	}
	// Late observations cannot rewrite recorded history.
	observe("one", 50, 50)
	again, _ := s.Availability(ctx, id, at, at.Add(660*time.Second))
	if again.Online != a.Online || again.Offline != a.Offline {
		t.Fatal("stale observation rewrote history")
	}
}
func TestMonitorMaintenanceCancellationUnknownAndDeletion(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	w := MonitorWindow{Kind: "maintenance", StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix()}
	if err := s.CreateMonitorWindow(ctx, &w, at); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelMonitorWindow(ctx, w.ID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	a, err := s.Availability(ctx, id, at, at.Add(time.Hour))
	if err != nil || a.Maintenance != 60 || a.Unknown != 3540 || a.Percent != nil || a.Coverage == nil || *a.Coverage != 0 {
		t.Fatalf("cancel %+v %v", a, err)
	}
	w = MonitorWindow{NodeID: &id, Kind: "silence", StartsAt: at.Add(time.Minute).Unix(), EndsAt: at.Add(time.Hour).Unix()}
	if err = s.CreateMonitorWindow(ctx, &w, at); err != nil {
		t.Fatal(err)
	}
	if err = s.CancelMonitorWindow(ctx, w.ID, at); err != nil {
		t.Fatal(err)
	}
	if muted, err := s.MonitorMuted(ctx, id, at.Add(2*time.Minute)); err != nil || muted {
		t.Fatal("canceled future window still mutes")
	}
	_, err = s.CheckIncident(ctx, id, IncidentCheck{Kind: "cpu", Active: true}, at.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	s.ObserveAvailability(ctx, id, "run", at, at, time.Minute)
	s.ObserveAvailability(ctx, id, "run", at.Add(time.Minute), at.Add(time.Minute), time.Minute)
	if _, err = s.DB().Exec(`DELETE FROM nodes WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"monitor_incidents", "monitor_observations", "monitor_intervals"} {
		var n int
		s.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		if n != 0 {
			t.Fatal("node records survived", table)
		}
	}
}
