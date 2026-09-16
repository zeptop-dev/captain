package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
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
