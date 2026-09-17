package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestInWindows(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 9, 17, h, m, 0, 0, time.Local) }
	if !InWindows(nil, at(3, 0)) {
		t.Fatal("empty = always")
	}
	w := []string{"20:00-02:00", "10:00-14:00"}
	for _, c := range []struct {
		h, m int
		want bool
	}{{21, 0, true}, {1, 59, true}, {2, 0, false}, {9, 59, false}, {10, 0, true}, {13, 59, true}, {14, 0, false}} {
		if got := InWindows(w, at(c.h, c.m)); got != c.want {
			t.Errorf("%02d:%02d = %v", c.h, c.m, got)
		}
	}
	if err := ValidateWindows([]string{"20:00-02:00"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWindows([]string{"20-02"}); err == nil {
		t.Fatal("bad window accepted")
	}
}

// A user averaging above the trigger over the window is throttled once
// (not again while the limit runs); whitelisted users never are; the
// throttle shows up in the node spec speeds and lapses on its own.
func TestDynLimitObserve(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	ctx := context.Background()
	u := &domain.User{Email: "fast@example.com", UUID: "u1", SubToken: "t1", Status: "active"}
	w := &domain.User{Email: "vip@example.com", UUID: "u2", SubToken: "t2", Status: "active"}
	_ = st.CreateUser(ctx, u)
	_ = st.CreateUser(ctx, w)
	_ = st.SetSetting(ctx, store.SettingDynLimit, store.DynLimitSettings{Enabled: true, TriggerMbps: 10, TriggerSeconds: 120, LimitMbps: 2, LimitSeconds: 300, Whitelist: []int64{w.ID}, ThrottleUnlimited: true})
	at := time.Date(2026, 9, 17, 12, 0, 30, 0, time.UTC)
	d := &DynLimit{Store: st, Now: func() time.Time { return at }}
	// 10 Mbps over 120 s is 150 MB; one minute of 100 MB is 14 Mbps but
	// only half the window, so the first report must not throttle.
	mb := int64(100 << 20)
	got := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: mb}, {UserID: w.ID, Down: mb}}, 60, at)
	if len(got) != 0 {
		t.Fatalf("throttled on half a window: %v", got)
	}
	at = at.Add(time.Minute)
	// A second minute fills the window: 200 MB over 120 s is 14 Mbps.
	got = d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: mb}, {UserID: w.ID, Down: mb}}, 60, at)
	if len(got) != 1 || got[0] != u.ID {
		t.Fatalf("throttled = %v", got)
	}
	if again := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: mb}}, 60, at); len(again) != 0 {
		t.Fatal("throttled twice")
	}
	lim, err := st.DynLimits(ctx, at)
	if err != nil || lim[u.ID] != 2 || lim[w.ID] != 0 {
		t.Fatalf("limits = %v %v", lim, err)
	}
	if n, _ := st.ClearExpiredDynLimits(ctx, at.Add(301*time.Second)); n != 1 {
		t.Fatalf("expired = %d", n)
	}
	if lim, _ = st.DynLimits(ctx, at.Add(301*time.Second)); len(lim) != 0 {
		t.Fatal("limit did not lapse")
	}
}

// A backlog report (one batch covering many minutes after an outage) is a
// rate over its own window, not a spike: an ordinary user must not be
// throttled just because the panel was restarted.
func TestDynLimitBacklogIsNotASpike(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	ctx := context.Background()
	u := &domain.User{Email: "steady@example.com", UUID: "u3", SubToken: "t3", Status: "active"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	_ = st.SetSetting(ctx, store.SettingDynLimit, store.DynLimitSettings{Enabled: true, TriggerMbps: 100, TriggerSeconds: 60, LimitMbps: 30, LimitSeconds: 600, ThrottleUnlimited: true})
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	d := &DynLimit{Store: st, Now: func() time.Time { return at }}
	// 12 minutes of 10 Mbps arriving in one batch: 900 MB over 720 s.
	if got := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: 900 << 20}}, 720, at); len(got) != 0 {
		t.Fatalf("backlog throttled an ordinary user: %v", got)
	}
	// The same amount really inside one minute is a spike and throttles.
	at = at.Add(time.Hour)
	if got := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: 900 << 20}}, 60, at); len(got) != 1 {
		t.Fatalf("a real spike was not throttled: %v", got)
	}
}
