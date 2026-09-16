package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// A known device is always admitted (and refreshed); a new one only while
// under the limit; forgetting a device frees its slot; limit 0 is
// unlimited; the per-user override is stored as nullable.
func TestHwidDevices(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	u := &domain.User{Email: "h@example.com", UUID: "u1", SubToken: "t1", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	claim := func(hwid string, limit int, at time.Time) (bool, int) {
		ok, n, err := s.ClaimHwidDevice(ctx, u.ID, HwidDevice{Hwid: hwid, Platform: "ios", UserAgent: "Happ/2.0"}, limit, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok, n
	}
	if ok, n := claim("AAAAAAAAAAAA", 2, at); !ok || n != 1 {
		t.Fatalf("first device: ok=%v n=%d", ok, n)
	}
	if ok, n := claim("BBBBBBBBBBBB", 2, at); !ok || n != 2 {
		t.Fatalf("second device: ok=%v n=%d", ok, n)
	}
	if ok, n := claim("CCCCCCCCCCCC", 2, at); ok || n != 2 {
		t.Fatalf("third device over limit: ok=%v n=%d", ok, n)
	}
	// Known device keeps working and its last_seen moves.
	later := at.Add(time.Hour)
	if ok, _ := claim("AAAAAAAAAAAA", 2, later); !ok {
		t.Fatal("known device refused")
	}
	devs, _ := s.HwidDevices(ctx, u.ID)
	if len(devs) != 2 {
		t.Fatalf("devices = %d, want 2 (refused device must not be stored)", len(devs))
	}
	for _, d := range devs {
		if d.Hwid == "AAAAAAAAAAAA" && !d.LastSeenAt.Equal(later) {
			t.Fatalf("last_seen not refreshed: %v", d.LastSeenAt)
		}
	}
	// Kick one, the slot frees.
	if err := s.DeleteHwidDevice(ctx, u.ID, "BBBBBBBBBBBB"); err != nil {
		t.Fatal(err)
	}
	if ok, n := claim("CCCCCCCCCCCC", 2, later); !ok || n != 2 {
		t.Fatalf("after kick: ok=%v n=%d", ok, n)
	}
	// Unlimited.
	if ok, _ := claim("DDDDDDDDDDDD", 0, later); !ok {
		t.Fatal("limit 0 must be unlimited")
	}
	// Per-user override round-trips through the user row.
	five := 5
	if err := s.SetUserHwidLimit(ctx, u.ID, &five); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.HwidLimit == nil || *got.HwidLimit != 5 {
		t.Fatalf("hwid_limit = %v", got.HwidLimit)
	}
	if err := s.SetUserHwidLimit(ctx, u.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.UserByID(ctx, u.ID)
	if got.HwidLimit != nil {
		t.Fatalf("hwid_limit should be null, got %d", *got.HwidLimit)
	}
	// Request history: recorded, listed newest first, pruned by age.
	for i, resp := range []string{"clash", "hwid-denied", "base64"} {
		if err := s.RecordSubRequest(ctx, u.ID, SubRequest{At: at.Add(time.Duration(i) * time.Minute), RequestIP: "203.0.113.30", UserAgent: "x", Response: resp}); err != nil {
			t.Fatal(err)
		}
	}
	reqs, _ := s.SubRequests(ctx, u.ID, 10)
	if len(reqs) != 3 || reqs[0].Response != "base64" {
		t.Fatalf("requests = %+v", reqs)
	}
	if _, err := s.PruneSubRequests(ctx, at.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	if reqs, _ = s.SubRequests(ctx, u.ID, 10); len(reqs) != 1 {
		t.Fatalf("after prune = %d, want 1", len(reqs))
	}
}
