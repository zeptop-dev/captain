package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// Traffic thresholds fire once each per quota period; the first traffic
// sample stamps first_connected_at and is reported once; users with a
// plan who never connected are listed once.
func TestThresholdsAndFirstConnected(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	plan := &domain.Plan{Name: "P", PeriodDays: 30, QuotaBytes: 1000, Enabled: true}
	if err := s.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	u := &domain.User{Email: "t@example.com", UUID: "u", SubToken: "s", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET created_at = ? WHERE id = ?`, at.Add(-48*time.Hour).Unix(), u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantSubscription(ctx, u.ID, plan, at); err != nil {
		t.Fatal(err)
	}
	idle, err := s.NeverConnectedUsers(ctx, at.Add(-24*time.Hour))
	if err != nil || len(idle) != 1 {
		t.Fatalf("never connected = %v %v", idle, err)
	}
	_ = s.MarkNotified(ctx, u.ID, "not_connected", "1")
	if idle, _ = s.NeverConnectedUsers(ctx, at.Add(-24*time.Hour)); len(idle) != 0 {
		t.Fatalf("not_connected listed twice")
	}
	first, err := s.AddTrafficSamples(ctx, []TrafficSample{{UserID: u.ID, Down: 650}}, at)
	if err != nil || len(first) != 1 || first[0] != u.ID {
		t.Fatalf("first connected = %v %v", first, err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.FirstConnectedAt == nil || !got.FirstConnectedAt.Equal(at) {
		t.Fatalf("first_connected_at = %v", got.FirstConnectedAt)
	}
	if first, _ = s.AddTrafficSamples(ctx, []TrafficSample{{UserID: u.ID, Down: 200}}, at.Add(time.Minute)); len(first) != 0 {
		t.Fatal("first connected reported twice")
	}
	// 85 % used: 60 and 80 cross, 90 does not.
	for _, c := range []struct{ pct, want int }{{90, 0}, {80, 1}, {60, 1}} {
		high, err := s.HighTrafficSubscriptions(ctx, c.pct)
		if err != nil || len(high) != c.want {
			t.Fatalf("threshold %d: %d hits (%v), want %d", c.pct, len(high), err, c.want)
		}
		for _, h := range high {
			if h.Threshold != c.pct || h.UsedPct != 85 {
				t.Fatalf("threshold %d: %+v", c.pct, h)
			}
			_ = s.MarkNotified(ctx, h.UserID, "traffic", h.Ref)
		}
	}
	if high, _ := s.HighTrafficSubscriptions(ctx, 80); len(high) != 0 {
		t.Fatal("80 notified twice")
	}
	// Legacy reference (pre-threshold format) still silences 90.
	if _, err := s.AddTrafficSamples(ctx, []TrafficSample{{UserID: u.ID, Down: 100}}, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	high, _ := s.HighTrafficSubscriptions(ctx, 90)
	if len(high) != 1 {
		t.Fatalf("90 hits = %d", len(high))
	}
	legacy := high[0].Ref[:len(high[0].Ref)-len(":90")]
	_ = s.MarkNotified(ctx, u.ID, "traffic", legacy)
	if high, _ = s.HighTrafficSubscriptions(ctx, 90); len(high) != 0 {
		t.Fatal("legacy 90 reference ignored")
	}
}
