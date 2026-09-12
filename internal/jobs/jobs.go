// Package jobs runs Captain's periodic housekeeping in-process.
package jobs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Runner executes housekeeping on a fixed interval.
type Runner struct {
	Store        *store.Store
	Log          *slog.Logger
	Interval     time.Duration // default 1m
	OrderTTL     time.Duration // pending orders older than this are cancelled; default 30m
	OnlineRetain time.Duration // online_devices rows older than this are purged; default 10m
	// BackupDir receives a daily database snapshot (captain-YYYY-MM-DD.db);
	// the newest BackupKeep files are kept. Empty disables backups.
	BackupDir  string
	BackupKeep int // default 7
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
	if r.BackupDir != "" {
		if made, err := r.backup(ctx, now); err != nil {
			log.Error("backup failed", "err", err)
		} else if made != "" {
			log.Info("database backed up", "file", made)
		}
	}
}

// backup takes today's snapshot if it does not exist yet and prunes old ones.
func (r *Runner) backup(ctx context.Context, now time.Time) (string, error) {
	keep := r.BackupKeep
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(r.BackupDir, 0o750); err != nil {
		return "", err
	}
	name := filepath.Join(r.BackupDir, "captain-"+now.Format("2006-01-02")+".db")
	if _, err := os.Stat(name); err == nil {
		return "", nil
	}
	if err := r.Store.Backup(ctx, name); err != nil {
		return "", err
	}
	files, _ := filepath.Glob(filepath.Join(r.BackupDir, "captain-*.db"))
	sort.Strings(files)
	for len(files) > keep {
		_ = os.Remove(files[0])
		files = files[1:]
	}
	return name, nil
}
