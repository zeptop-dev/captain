package jobs

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestTick(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	ctx := context.Background()
	u := &domain.User{Email: "a@test", PasswordHash: "x", Role: "user", UUID: "u", SubToken: "t", Status: "active"}
	_ = st.CreateUser(ctx, u)
	plan := &domain.Plan{Name: "p", PriceCents: 100, PeriodDays: 30, QuotaBytes: 1000, ResetDays: 7, Enabled: true}
	_ = st.CreatePlan(ctx, plan)
	// Subscription granted 10 days ago: the weekly reset is overdue.
	start := time.Now().AddDate(0, 0, -10)
	sub, err := st.GrantSubscription(ctx, u.ID, plan, start)
	if err != nil || sub.ResetAt == nil {
		t.Fatalf("grant: %+v %v", sub, err)
	}
	_ = st.AddTraffic(ctx, u.ID, 1, 400, 500, time.Now())
	// A stale pending order and a fresh one.
	_ = st.CreateOrder(ctx, &domain.Order{No: "old", UserID: u.ID, PlanID: plan.ID, AmountCents: 100, Gateway: "epay"})
	conn.Exec(`UPDATE orders SET created_at = ? WHERE no = 'old'`, time.Now().Add(-2*time.Hour).Unix())
	_ = st.CreateOrder(ctx, &domain.Order{No: "new", UserID: u.ID, PlanID: plan.ID, AmountCents: 100, Gateway: "epay"})

	(&Runner{Store: st, Log: slog.Default()}).Tick(ctx)

	sub, _ = st.ActiveSubscription(ctx, u.ID)
	if sub.UsedUpBytes != 0 || sub.UsedDownBytes != 0 {
		t.Fatalf("quota not reset: %+v", sub)
	}
	if sub.ResetAt == nil || !sub.ResetAt.After(time.Now()) {
		t.Fatalf("next reset must be in the future: %v", sub.ResetAt)
	}
	// Next reset is start + 14d (two weekly steps).
	if want := start.AddDate(0, 0, 14); sub.ResetAt.Unix() != want.Unix() {
		t.Fatalf("reset_at = %v, want %v", sub.ResetAt, want)
	}
	old, err := st.OrderByNo(ctx, "old")
	if err != nil {
		t.Fatalf("order old: %v", err)
	}
	fresh, err := st.OrderByNo(ctx, "new")
	if err != nil {
		t.Fatalf("order new: %v", err)
	}
	if old.Status != "cancelled" || fresh.Status != "pending" {
		t.Fatalf("orders: old=%s new=%s", old.Status, fresh.Status)
	}

	// Expiry: a plan that already ended.
	short := &domain.Plan{Name: "s", PriceCents: 1, PeriodDays: 1, Enabled: true}
	_ = st.CreatePlan(ctx, short)
	_, _ = st.GrantSubscription(ctx, u.ID, short, time.Now().AddDate(0, 0, -2))
	(&Runner{Store: st, Log: slog.Default()}).Tick(ctx)
	if _, err := st.ActiveSubscription(ctx, u.ID); err == nil {
		t.Fatal("expired subscription still active")
	}
}

func TestBackup(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	dir := filepath.Join(t.TempDir(), "backups")
	r := &Runner{Store: st, Log: slog.Default(), BackupDir: dir, BackupKeep: 2}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := r.backup(context.Background(), now.AddDate(0, 0, i)); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "captain-*.db"))
	if len(files) != 2 {
		t.Fatalf("expected 2 kept backups, got %v", files)
	}
	if made, _ := r.backup(context.Background(), now.AddDate(0, 0, 2)); made != "" {
		t.Fatal("same day should not back up twice")
	}
	// The snapshot is a usable database.
	c2, err := db.Open("sqlite", files[1])
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := c2.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("backup not readable: %v", err)
	}
}
