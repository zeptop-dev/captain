package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
)

// Alerts raised within the window go out as one message; a lone alert is
// sent as is.
func TestAlertBatching(t *testing.T) {
	var mu sync.Mutex
	var got []string
	p := &Probe{Notify: &notify.Notifier{}, AlertWindow: 30 * time.Millisecond}
	p.adminSend = func(_ context.Context, text string) { mu.Lock(); got = append(got, text); mu.Unlock() }
	for _, n := range []string{"a", "b", "c"} {
		p.notify(context.Background(), &domain.Node{Name: n}, "offline", "🔴 "+n+" is offline")
	}
	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	if len(got) != 1 || !strings.HasPrefix(got[0], "📣 3 node alerts\n") || strings.Count(got[0], "is offline") != 3 {
		t.Fatalf("batched = %q", got)
	}
	got = nil
	mu.Unlock()
	p.notify(context.Background(), &domain.Node{Name: "d"}, "recovered", "✅ d is back online")
	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "✅ d is back online" {
		t.Fatalf("single = %q", got)
	}
}

// A fleet-wide alert has to fit into one Telegram message.
func TestAlertBatchFitsTelegram(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = strings.Repeat("x", 60) + " is offline (last seen 5m0s ago)"
	}
	msg := batchText(lines)
	if len(msg) > tgLimit {
		t.Fatalf("message is %d bytes, over the %d limit", len(msg), tgLimit)
	}
	if !strings.HasPrefix(msg, "📣 300 node alerts") || !strings.Contains(msg, "… and ") {
		t.Fatalf("truncated message does not say what it left out:\n%s", msg)
	}
	// A short batch is not touched.
	if got := batchText([]string{"a", "b"}); got != "📣 2 node alerts\na\nb" {
		t.Fatalf("short batch = %q", got)
	}
}

// After a restart longer than the offline grace, nodes get one grace
// period to beat again before anything is called offline.
func TestOfflineGraceAfterRestart(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	node := &domain.Node{Name: "n1"}
	if err := st.CreateNode(ctx, node, "paircode", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RedeemPairCode(ctx, "paircode", "hash", "h1", "v1", "linux"); err != nil {
		t.Fatal(err)
	}
	// The panel was down for an hour, so the stored last beat is that old.
	if _, err := conn.ExecContext(ctx, `UPDATE nodes SET last_seen_at = ?`, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	var sent []string
	p := &Probe{Store: st, Notify: &notify.Notifier{}, AlertWindow: -1}
	p.adminSend = func(_ context.Context, text string) { sent = append(sent, text) }
	var ps store.ProbeSettings
	ps.Enabled, ps.Alerts.OfflineSeconds = true, 180
	if err := st.SetSetting(ctx, store.SettingProbe, ps); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p.CheckOffline(ctx, now)
	if len(sent) != 0 {
		t.Fatalf("alerted right after the restart: %q", sent)
	}
	p.CheckOffline(ctx, now.Add(170*time.Second)) // still inside the grace
	if len(sent) != 0 {
		t.Fatalf("alerted inside the grace period: %q", sent)
	}
	p.CheckOffline(ctx, now.Add(200*time.Second)) // past it, still silent node
	if len(sent) != 1 || !strings.Contains(sent[0], "offline") {
		t.Fatalf("no alert after the grace period: %q", sent)
	}
}
