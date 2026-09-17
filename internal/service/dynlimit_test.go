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
	_ = st.SetSetting(ctx, store.SettingDynLimit, store.DynLimitSettings{Enabled: true, TriggerMbps: 10, TriggerSeconds: 120, LimitMbps: 2, LimitSeconds: 300, Whitelist: []int64{w.ID}})
	at := time.Date(2026, 9, 17, 12, 0, 30, 0, time.UTC)
	d := &DynLimit{Store: st, Now: func() time.Time { return at }}
	// 10 Mbps over 120 s = 150 MB; feed 100 MB per minute for two minutes.
	mb := int64(100 << 20)
	for i := 0; i < 2; i++ {
		got := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: mb}, {UserID: w.ID, Down: mb}}, at)
		if len(got) != 0 {
			t.Fatalf("minute %d: throttled early: %v", i, got)
		}
		at = at.Add(time.Minute)
	}
	// Next report carries nothing for the fast user (the burst ended), but
	// the two completed minutes average 13 Mbps, so it is throttled now.
	got := d.Observe(ctx, nil, at)
	if len(got) != 1 || got[0] != u.ID {
		t.Fatalf("throttled = %v", got)
	}
	if again := d.Observe(ctx, []store.TrafficSample{{UserID: u.ID, Down: mb}}, at); len(again) != 0 {
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
