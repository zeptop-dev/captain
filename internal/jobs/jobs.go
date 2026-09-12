// Package jobs runs Captain's periodic housekeeping in-process.
package jobs

import (
	"context"
	"github.com/zeptop-dev/captain/internal/mail"
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
	// Mail enables expiry/traffic reminders when the settings allow them.
	Mail      *mail.Loader
	SiteName  string
	PortalURL string

	lastReminders time.Time
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
	if r.Mail != nil && now.Sub(r.lastReminders) >= time.Hour {
		r.lastReminders = now
		r.reminders(ctx, now, log)
	}
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

// reminders mails users whose subscription expires within three days or
// whose quota is 90% used, once per expiry / quota period.
func (r *Runner) reminders(ctx context.Context, now time.Time, log *slog.Logger) {
	ms := r.Mail.Settings(ctx)
	if !ms.Enabled() || !ms.Reminders {
		return
	}
	exp, err := r.Store.ExpiringSubscriptions(ctx, now, 3*24*time.Hour)
	if err != nil {
		log.Error("expiry reminders", "err", err)
	}
	for _, e := range exp {
		if err := mail.Send(ctx, ms, mail.ExpiryMessage(r.SiteName, e.Email, r.PortalURL, e.ExpiresAt)); err != nil {
			log.Warn("expiry reminder", "to", e.Email, "err", err)
			continue
		}
		_ = r.Store.MarkNotified(ctx, e.UserID, "expiry", e.Ref)
	}
	high, err := r.Store.HighTrafficSubscriptions(ctx, 90)
	if err != nil {
		log.Error("traffic reminders", "err", err)
	}
	for _, e := range high {
		if err := mail.Send(ctx, ms, mail.TrafficMessage(r.SiteName, e.Email, r.PortalURL, e.UsedPct)); err != nil {
			log.Warn("traffic reminder", "to", e.Email, "err", err)
			continue
		}
		_ = r.Store.MarkNotified(ctx, e.UserID, "traffic", e.Ref)
	}
	if len(exp)+len(high) > 0 {
		log.Info("reminders sent", "expiry", len(exp), "traffic", len(high))
	}
}
