// Package jobs runs Captain's periodic housekeeping in-process.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"gitlab.com/boyang-hu/captain/internal/store"
)

// Runner executes housekeeping on a fixed interval.
type Runner struct {
	Store        *store.Store
	Log          *slog.Logger
	Interval     time.Duration // default 1m
	OrderTTL     time.Duration // pending orders older than this are cancelled; default 30m
	OnlineRetain time.Duration // online_devices rows older than this are purged; default 10m
}

// Run blocks until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	if r.Interval == 0 {
		r.Interval = time.Minute
	}
	if r.OrderTTL == 0 {
		r.OrderTTL = 30 * time.Minute
	}
	if r.OnlineRetain == 0 {
		r.OnlineRetain = 10 * time.Minute
	}
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	r.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Tick(ctx)
		}
	}
}

// Tick runs every job once.
func (r *Runner) Tick(ctx context.Context) {
	now := time.Now()
	log := r.Log.With("component", "jobs")
	report := func(name string, n int64, err error) {
		if err != nil {
			log.Error(name+" failed", "err", err)
		} else if n > 0 {
			log.Info(name, "rows", n)
		}
	}
	n, err := r.Store.CancelStaleOrders(ctx, now.Add(-r.OrderTTL))
	report("cancelled stale orders", n, err)
	n, err = r.Store.ExpireSubscriptions(ctx, now)
	report("expired subscriptions", n, err)
	n, err = r.Store.ResetQuotas(ctx, now)
	report("reset quotas", n, err)
	n, err = r.Store.PurgeSessions(ctx, now)
	report("purged sessions", n, err)
	n, err = r.Store.PurgeOnline(ctx, now.Add(-r.OnlineRetain))
	report("purged online devices", n, err)
}
