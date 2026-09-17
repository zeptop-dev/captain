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
	// PushSeconds is the node report interval, used when an agent does
	// not say what window its deltas cover (before bosun 0.46).
	PushSeconds int
	// Now overrides the clock (tests).
	Now func() time.Time

	mu       sync.Mutex
	recent   map[int64][]sample // user -> the reports inside the trigger window
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
		// A read error keeps the previous value: caching the zero value
		// would turn one cancelled request into 15 seconds of "the
		// feature is off".
		if err := d.Store.GetSetting(ctx, store.SettingDynLimit, &s); err == nil {
			d.settings, d.fetched = s.Defaults(), d.now()
		} else if d.fetched.IsZero() {
			// Nothing cached yet: run on the defaults and retry next call.
			d.settings = s.Defaults()
		}
	}
	return d.settings
}

// Invalidate drops the settings cache (after an admin edit).
func (d *DynLimit) Invalidate() {
	d.mu.Lock()
	d.fetched = time.Time{}
	d.mu.Unlock()
}

// sample is one report's delta for a user: bytes accumulated over the
// window that ended at end.
type sample struct {
	end   time.Time
	win   time.Duration
	bytes int64
}

// Observe takes one node report's deltas. window is how long they have
// been accumulating (Report.TrafficWindowSeconds); 0 falls back to the
// panel's push interval. It returns the ids of users throttled just now.
func (d *DynLimit) Observe(ctx context.Context, samples []store.TrafficSample, window int, at time.Time) []int64 {
	if d == nil {
		return nil
	}
	s := d.Settings(ctx)
	if !s.Enabled || !InWindows(s.Windows, at) {
		return nil
	}
	win := time.Duration(window) * time.Second
	if win <= 0 {
		win = time.Duration(d.PushSeconds) * time.Second
	}
	if win <= 0 {
		win = time.Minute
	}
	trigger := time.Duration(s.TriggerSeconds) * time.Second
	white := map[int64]bool{}
	for _, id := range s.Whitelist {
		white[id] = true
	}
	d.mu.Lock()
	if d.recent == nil {
		d.recent = map[int64][]sample{}
	}
	for _, sm := range samples {
		if sm.Up <= 0 && sm.Down <= 0 {
			continue
		}
		d.recent[sm.UserID] = append(d.recent[sm.UserID], sample{end: at, win: win, bytes: sm.Up + sm.Down})
	}
	// A user's rate is the traffic that actually falls inside the trigger
	// window, each report counted for the part of its own window that
	// overlaps: a backlog covering twenty minutes is twenty minutes of
	// traffic, not one minute's spike.
	from := at.Add(-trigger)
	var over []int64
	var rates []int
	for uid, list := range d.recent {
		var bits float64
		kept := list[:0]
		for _, sm := range list {
			start := sm.end.Add(-sm.win)
			if sm.end.After(from) {
				kept = append(kept, sm)
			} else {
				continue // entirely before the window
			}
			lo := start
			if lo.Before(from) {
				lo = from
			}
			overlap := sm.end.Sub(lo)
			if overlap <= 0 || sm.win <= 0 {
				continue
			}
			bits += float64(sm.bytes) * 8 * (float64(overlap) / float64(sm.win))
		}
		if len(kept) == 0 {
			delete(d.recent, uid)
			continue
		}
		d.recent[uid] = kept
		mbps := int(bits / trigger.Seconds() / 1e6)
		if mbps >= s.TriggerMbps && !white[uid] {
			over = append(over, uid)
			rates = append(rates, mbps)
		}
	}
	d.mu.Unlock()
	var throttled []int64
	for i, uid := range over {
		if cur, err := d.Store.DynLimitFor(ctx, uid, at); err == nil && cur != nil {
			continue // already limited; wait for it to lapse
		}
		// Throttling a user who has no speed limit at all adds a marking
		// outbound to every core that serves them, which means a restart
		// and a moment of dropped connections for everybody on those
		// nodes. By default such users are left alone and only the log
		// says so; ThrottleUnlimited accepts the restart.
		if !s.ThrottleUnlimited {
			limits, err := d.Store.SpeedLimits(ctx)
			if err == nil && limits[uid] == 0 {
				if d.Log != nil {
					d.Log.Info("user over the dynamic limit but has no speed limit; not throttled (dynlimit.throttle_unlimited is off)", "user", uid, "rate_mbps", rates[i])
				}
				continue
			}
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
			d.Notify.AdminAsync(fmt.Sprintf("🐢 %s throttled to %d Mbps for %s (averaged %d Mbps over %ds)", notify.Escape(email), s.LimitMbps, (time.Duration(s.LimitSeconds) * time.Second).String(), rates[i], s.TriggerSeconds))
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
