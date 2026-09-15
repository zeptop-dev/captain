// Package jobs runs Captain's periodic housekeeping in-process.
package jobs

import (
	"context"
	"fmt"
	"github.com/zeptop-dev/captain/internal/backup"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/telegram"
	"github.com/zeptop-dev/captain/internal/webhook"
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
	// Backups, when set, replaces both with the manager (remote upload,
	// configurable hour and retention).
	BackupDir  string
	BackupKeep int // default 7
	Backups    *backup.Manager
	// Certs renews panel-issued certificates (nil = off).
	Certs *service.Certs
	// Mail enables expiry/traffic reminders when the settings allow them.
	Mail *mail.Loader
	// Bot delivers reminders to users who linked Telegram (nil = off).
	Bot *telegram.Bot
	// Hooks receives subscription.expiring events (nil = off).
	Hooks *webhook.Hub
	// Probe raises offline notices and prunes metrics (nil = off).
	Probe *service.Probe
	// External re-syncs airport subscriptions hourly (nil = off).
	External  *service.External
	lastSync  time.Time
	lastPrune time.Time
	lastProbe time.Time
	lastHist  time.Time
	// Invalidate drops cached node state after the tick changed
	// subscriptions (expiry, resets, queued starts); nil = none.
	Invalidate func()
	SiteName   string
	PortalURL  string

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
	n, err = r.Store.PromoteQueued(ctx, now)
	report("started queued subscriptions", n, err)
	n, err = r.Store.ResetQuotas(ctx, now)
	report("reset quotas", n, err)
	n, err = r.Store.PurgeSessions(ctx, now)
	report("purged sessions", n, err)
	n, err = r.Store.PurgeOnline(ctx, now.Add(-r.OnlineRetain))
	report("purged online devices", n, err)
	if r.External != nil && now.Sub(r.lastSync) >= time.Hour {
		r.lastSync = now
		r.External.SyncAll(ctx, now)
	}
	if r.External != nil && now.Sub(r.lastProbe) >= 10*time.Minute {
		r.lastProbe = now
		if up, total := r.External.ProbeAll(ctx, now); total > 0 {
			log.Info("external nodes probed", "up", up, "total", total)
		}
	}
	if now.Sub(r.lastHist) >= 24*time.Hour {
		r.lastHist = now
		n, err = r.Store.PruneHistory(ctx, now, 400)
		report("pruned traffic history", n, err)
	}
	if r.Invalidate != nil {
		r.Invalidate()
	}
	if r.Probe != nil {
		r.Probe.CheckOffline(ctx, now)
		if now.Sub(r.lastPrune) >= time.Hour {
			r.lastPrune = now
			if err := r.Store.PruneStats(ctx, now); err != nil {
				log.Error("prune stats", "err", err)
			}
		}
	}
	if r.Mail != nil && now.Sub(r.lastReminders) >= time.Hour {
		r.lastReminders = now
		r.reminders(ctx, now, log)
	}
	if r.Certs != nil {
		r.Certs.RenewDue(ctx)
	}
	if r.Backups != nil {
		r.Backups.Tick(ctx)
	} else if r.BackupDir != "" {
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
	viaMail := ms.Enabled() && ms.Reminders
	viaBot := r.Bot != nil && r.Bot.Enabled(ctx)
	if !viaMail && !viaBot && r.Hooks == nil {
		return
	}
	// deliver tries Telegram first, then mail; false when neither could.
	deliver := func(userID int64, m mail.Message, short string) bool {
		if viaBot {
			if sent, err := r.Bot.NotifyUser(ctx, userID, short); err == nil && sent {
				return true
			}
		}
		if viaMail {
			if err := mail.Send(ctx, ms, m); err != nil {
				log.Warn("reminder mail", "to", m.To, "err", err)
				return false
			}
			return true
		}
		return false
	}
	exp, err := r.Store.ExpiringSubscriptions(ctx, now, 3*24*time.Hour)
	if err != nil {
		log.Error("expiry reminders", "err", err)
	}
	for _, e := range exp {
		r.Hooks.Emit(ctx, webhook.SubscriptionExpiring, map[string]any{"user_id": e.UserID, "email": e.Email, "expires_at": e.ExpiresAt})
		if !deliver(e.UserID, mail.ExpiryMessage(r.SiteName, e.Email, r.PortalURL, e.ExpiresAt), fmt.Sprintf("⏰ %s: your plan expires on %s. Renew: %s", r.SiteName, e.ExpiresAt.Format("2006-01-02"), r.PortalURL)) {
			continue
		}
		_ = r.Store.MarkNotified(ctx, e.UserID, "expiry", e.Ref)
	}
	high, err := r.Store.HighTrafficSubscriptions(ctx, 90)
	if err != nil {
		log.Error("traffic reminders", "err", err)
	}
	for _, e := range high {
		if !deliver(e.UserID, mail.TrafficMessage(r.SiteName, e.Email, r.PortalURL, e.UsedPct), fmt.Sprintf("📊 %s: you have used %d%% of your traffic. %s", r.SiteName, e.UsedPct, r.PortalURL)) {
			continue
		}
		_ = r.Store.MarkNotified(ctx, e.UserID, "traffic", e.Ref)
	}
	if len(exp)+len(high) > 0 {
		log.Info("reminders sent", "expiry", len(exp), "traffic", len(high))
	}
}
