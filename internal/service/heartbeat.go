package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Heartbeat answers the question the probe cannot: who watches the panel?
// Every tick Captain fetches an operator-supplied URL, so an external
// watchdog that expects a regular ping — healthchecks.io, Uptime Kuma's
// push monitor, Better Stack, a cron sentinel — alerts when the panel
// stops, whether it crashed, ran out of disk or lost its network.
//
// It is deliberately a *pull from us*, not a listener: it works behind
// NAT, behind Cloudflare Tunnel and with no inbound ports at all, which
// is how a lot of these panels run.
type Heartbeat struct {
	Store *store.Store
	Log   *slog.Logger
	HTTP  *http.Client

	mu       sync.Mutex
	settings store.HeartbeatSettings
	fetched  time.Time
	last     store.HeartbeatStatus
}

// Settings returns the configuration with a short cache; a failed read
// keeps the previous value rather than silently switching the watchdog
// off for 15 seconds.
func (h *Heartbeat) Settings(ctx context.Context) store.HeartbeatSettings {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.fetched) >= 15*time.Second {
		var s store.HeartbeatSettings
		if err := h.Store.GetSetting(ctx, store.SettingHeartbeat, &s); err == nil {
			h.settings, h.fetched = s, time.Now()
		}
	}
	return h.settings
}

// Invalidate drops the settings cache (after an admin edit).
func (h *Heartbeat) Invalidate() {
	h.mu.Lock()
	h.fetched = time.Time{}
	h.mu.Unlock()
}

// Status is what the settings card shows: the last attempt and its result.
func (h *Heartbeat) Status() store.HeartbeatStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// Tick sends one beat when the configured interval has passed. It is
// called from the job runner every minute and returns quickly: a watchdog
// that is itself down must not hold up housekeeping.
func (h *Heartbeat) Tick(ctx context.Context, now time.Time) {
	s := h.Settings(ctx)
	if !s.Enabled || strings.TrimSpace(s.URL) == "" {
		return
	}
	h.mu.Lock()
	due := h.last.At.IsZero() || now.Sub(h.last.At) >= s.Every()
	h.mu.Unlock()
	if !due {
		return
	}
	h.send(ctx, s, now)
}

// Send beats once regardless of the interval (the "test" button).
func (h *Heartbeat) Send(ctx context.Context) error {
	s := h.Settings(ctx)
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("no heartbeat URL configured")
	}
	return h.send(ctx, s, time.Now())
}

func (h *Heartbeat) send(ctx context.Context, s store.HeartbeatSettings, now time.Time) error {
	cli := h.HTTP
	if cli == nil {
		cli = &http.Client{Timeout: 15 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(s.URL), nil)
	if err == nil {
		req.Header.Set("User-Agent", "captain-heartbeat")
	}
	st := store.HeartbeatStatus{At: now}
	if err == nil {
		var resp *http.Response
		resp, err = cli.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			st.Code = resp.StatusCode
			if resp.StatusCode >= 400 {
				err = fmt.Errorf("watchdog answered %s", resp.Status)
			}
		}
	}
	if err != nil {
		st.Error = err.Error()
		if h.Log != nil {
			h.Log.Warn("heartbeat not delivered", "err", err)
		}
	}
	h.mu.Lock()
	h.last = st
	h.mu.Unlock()
	return err
}
