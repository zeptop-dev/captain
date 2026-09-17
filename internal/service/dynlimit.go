package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/webhook"
)

// DynLimit is the dynamic speed limiter: it watches the traffic every node
// reports, keeps a per-user, per-minute tally across nodes, and when a
// user's average over the trigger window exceeds the threshold puts a
// temporary limit on them (store.DynLimit). The node spec then carries
// min(plan limit, temporary limit) and the nodes shape as usual.
type DynLimit struct {
	Store  *store.Store
	State  *AgentState
	Notify *notify.Notifier
	Hooks  *webhook.Hub
	Log    *slog.Logger
	// Now overrides the clock (tests).
	Now func() time.Time

	mu       sync.Mutex
	buckets  map[int64]map[int64]int64 // user -> minute -> bytes
	settings store.DynLimitSettings
	fetched  time.Time
}

func (d *DynLimit) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Settings returns the cached configuration (15 s).
func (d *DynLimit) Settings(ctx context.Context) store.DynLimitSettings {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.now().Sub(d.fetched) >= 15*time.Second {
		var s store.DynLimitSettings
		_ = d.Store.GetSetting(ctx, store.SettingDynLimit, &s)
		d.settings, d.fetched = s.Defaults(), d.now()
	}
	return d.settings
}

// Invalidate drops the settings cache (after an admin edit).
func (d *DynLimit) Invalidate() {
	d.mu.Lock()
	d.fetched = time.Time{}
	d.mu.Unlock()
}

// Observe takes one node report's samples. It returns the ids of users it
// throttled just now.
func (d *DynLimit) Observe(ctx context.Context, samples []store.TrafficSample, at time.Time) []int64 {
	if d == nil || len(samples) == 0 {
		return nil
	}
	s := d.Settings(ctx)
	if !s.Enabled || !InWindows(s.Windows, at) {
		return nil
	}
	minute := at.Unix() / 60
	windowMin := int64((s.TriggerSeconds + 59) / 60)
	if windowMin < 1 {
		windowMin = 1
	}
	white := map[int64]bool{}
	for _, id := range s.Whitelist {
		white[id] = true
	}
	d.mu.Lock()
	if d.buckets == nil {
		d.buckets = map[int64]map[int64]int64{}
	}
	touched := map[int64]bool{}
	for _, sm := range samples {
		if sm.Up+sm.Down <= 0 {
			continue
		}
		b := d.buckets[sm.UserID]
		if b == nil {
			b = map[int64]int64{}
			d.buckets[sm.UserID] = b
		}
		b[minute] += sm.Up + sm.Down
		touched[sm.UserID] = true
	}
	// Evaluate the completed minutes only; the current one is partial.
	from, to := minute-windowMin, minute-1
	var over []int64
	var rates []int
	for uid := range touched {
		b := d.buckets[uid]
		var sum int64
		for m, n := range b {
			if m < from {
				delete(b, m)
			} else if m <= to {
				sum += n
			}
		}
		mbps := int(sum * 8 / (windowMin * 60) / 1_000_000)
		if mbps >= s.TriggerMbps && !white[uid] {
			over = append(over, uid)
			rates = append(rates, mbps)
		}
	}
	for uid, b := range d.buckets {
		if len(b) == 0 && !touched[uid] {
			delete(d.buckets, uid)
		}
	}
	d.mu.Unlock()
	var throttled []int64
	for i, uid := range over {
		if cur, err := d.Store.DynLimitFor(ctx, uid, at); err == nil && cur != nil {
			continue // already limited; wait for it to lapse
		}
		until := at.Add(time.Duration(s.LimitSeconds) * time.Second)
		if err := d.Store.SetDynLimit(ctx, uid, s.LimitMbps, rates[i], at, until); err != nil {
			if d.Log != nil {
				d.Log.Error("dynamic limit", "user", uid, "err", err)
			}
			continue
		}
		throttled = append(throttled, uid)
		if d.Log != nil {
			d.Log.Info("user throttled", "user", uid, "rate_mbps", rates[i], "limit_mbps", s.LimitMbps, "until", until.Format(time.RFC3339))
		}
		email := ""
		if u, err := d.Store.UserByID(ctx, uid); err == nil {
			email = u.Email
		}
		d.Hooks.Emit(ctx, webhook.UserThrottled, map[string]any{"user_id": uid, "email": email, "rate_mbps": rates[i], "limit_mbps": s.LimitMbps, "until": until})
		if d.Notify != nil {
			d.Notify.Admin(ctx, fmt.Sprintf("🐢 %s throttled to %d Mbps for %s (averaged %d Mbps over %ds)", email, s.LimitMbps, (time.Duration(s.LimitSeconds) * time.Second).String(), rates[i], s.TriggerSeconds))
		}
	}
	if len(throttled) > 0 && d.State != nil {
		d.State.Invalidate()
	}
	return throttled
}

// InWindows reports whether t (local time) falls in one of the "HH:MM-HH:MM"
// ranges; an empty list means always. A range may cross midnight.
func InWindows(windows []string, t time.Time) bool {
	if len(windows) == 0 {
		return true
	}
	cur := t.Hour()*60 + t.Minute()
	for _, w := range windows {
		a, b, ok := strings.Cut(strings.TrimSpace(w), "-")
		if !ok {
			continue
		}
		start, err1 := minuteOfDay(a)
		end, err2 := minuteOfDay(b)
		if err1 != nil || err2 != nil {
			continue
		}
		if start <= end {
			if cur >= start && cur < end {
				return true
			}
		} else if cur >= start || cur < end {
			return true
		}
	}
	return false
}

func minuteOfDay(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("bad time %q", s)
	}
	hh, err := strconv.Atoi(h)
	if err != nil || hh < 0 || hh > 24 {
		return 0, fmt.Errorf("bad hour %q", s)
	}
	mm, err := strconv.Atoi(m)
	if err != nil || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("bad minute %q", s)
	}
	return hh*60 + mm, nil
}

// ValidateWindows checks the "HH:MM-HH:MM" list.
func ValidateWindows(windows []string) error {
	for _, w := range windows {
		a, b, ok := strings.Cut(strings.TrimSpace(w), "-")
		if !ok {
			return fmt.Errorf("window %q must be HH:MM-HH:MM", w)
		}
		if _, err := minuteOfDay(a); err != nil {
			return err
		}
		if _, err := minuteOfDay(b); err != nil {
			return err
		}
	}
	return nil
}
