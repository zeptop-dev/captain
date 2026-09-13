package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/webhook"
)

// Probe keeps the live view of every node (latest beat plus a short ring
// for sparklines), folds beats into the store and raises alerts.
type Probe struct {
	Store  *store.Store
	Notify *notify.Notifier
	Log    *slog.Logger

	mu       sync.Mutex
	live     map[int64]*Live
	settings store.ProbeSettings
	fetched  time.Time
	gen      uint64 // bumped on every settings change; the snapshot cache keys on it
}

// Gen is the configuration generation (see Invalidate).
func (p *Probe) Gen() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen
}

// Live is a node's most recent state.
type Live struct {
	Host    spec.SystemStatus `json:"host"`
	At      time.Time         `json:"at"`
	Version string            `json:"version"`
	Ring    []Sample          `json:"-"`
}

// Sample is one point of the in-memory sparkline ring.
type Sample struct {
	At      int64   `json:"t"`
	CPU     float64 `json:"cpu"`
	MemPct  float64 `json:"mem"`
	NetUp   uint64  `json:"up"`
	NetDown uint64  `json:"down"`
}

const ringMax = 720 // 1h at 5s, 2h at 10s

// Settings returns the probe settings with a short cache.
func (p *Probe) Settings(ctx context.Context) store.ProbeSettings {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.fetched) < 10*time.Second {
		return p.settings
	}
	var s store.ProbeSettings
	_ = p.Store.GetSetting(ctx, store.SettingProbe, &s)
	s.Normalize()
	p.settings, p.fetched = s, time.Now()
	return s
}

// Invalidate drops the settings cache and bumps the generation.
func (p *Probe) Invalidate() {
	p.mu.Lock()
	p.fetched = time.Time{}
	p.gen++
	p.mu.Unlock()
}

// AgentConfig is what a node receives in its state.
func (p *Probe) AgentConfig(ctx context.Context, nodeID int64) *spec.Probe {
	s := p.Settings(ctx)
	if !s.Enabled {
		return nil
	}
	cfg := &spec.Probe{Enabled: true, BeatSeconds: s.BeatSeconds, CarrierPing: s.CarrierPing}
	tasks, _ := p.Store.ListPingTasks(ctx)
	for _, t := range tasks {
		if !t.Enabled {
			continue
		}
		if len(t.NodeIDs) > 0 {
			mine := false
			for _, id := range t.NodeIDs {
				if id == nodeID {
					mine = true
					break
				}
			}
			if !mine {
				continue
			}
		}
		cfg.Tasks = append(cfg.Tasks, spec.PingTask{ID: t.ID, Name: t.Name, Type: t.Type, Target: t.Target, IntervalSeconds: t.IntervalSeconds})
	}
	return cfg
}

// Record takes one beat from a node.
func (p *Probe) Record(ctx context.Context, n *domain.Node, version string, host spec.SystemStatus, at time.Time) error {
	s := p.Settings(ctx)
	if !s.Enabled {
		return nil
	}
	p.mu.Lock()
	if p.live == nil {
		p.live = map[int64]*Live{}
	}
	l := p.live[n.ID]
	if l == nil {
		l = &Live{}
		p.live[n.ID] = l
	}
	wasOffline := !l.At.IsZero() && at.Sub(l.At) > time.Duration(s.Alerts.OfflineSeconds)*time.Second
	l.Host, l.At, l.Version = host, at, version
	memPct := 0.0
	if host.MemTotal > 0 {
		memPct = float64(host.MemUsed) * 100 / float64(host.MemTotal)
	}
	l.Ring = append(l.Ring, Sample{At: at.Unix(), CPU: host.CPUPercent, MemPct: memPct, NetUp: host.NetUp, NetDown: host.NetDown})
	if len(l.Ring) > ringMax {
		l.Ring = l.Ring[len(l.Ring)-ringMax:]
	}
	p.mu.Unlock()
	if err := p.Store.RecordBeat(ctx, n.ID, host, at); err != nil {
		return err
	}
	if wasOffline {
		_ = p.Store.ClearAlert(ctx, n.ID, "offline")
		p.notify(ctx, n, "recovered", "✅ "+n.Name+" is back online")
	}
	p.thresholds(ctx, n, s, at)
	return nil
}

func (p *Probe) thresholds(ctx context.Context, n *domain.Node, s store.ProbeSettings, at time.Time) {
	window := time.Duration(s.Alerts.WindowMinutes) * time.Minute
	for _, m := range []struct {
		kind  string
		limit int
	}{{"cpu", s.Alerts.CPUPct}, {"mem", s.Alerts.MemPct}, {"disk", s.Alerts.DiskPct}} {
		if m.limit <= 0 {
			continue
		}
		avg, ok := p.Store.SustainedAverage(ctx, n.ID, m.kind, window, at)
		if !ok || avg < float64(m.limit) {
			continue
		}
		if fire, _ := p.Store.AlertOnce(ctx, n.ID, m.kind, time.Hour, at); fire {
			p.notify(ctx, n, m.kind, fmt.Sprintf("⚠️ %s: %s at %.0f%% over the last %d min (limit %d%%)", n.Name, strings.ToUpper(m.kind), avg, s.Alerts.WindowMinutes, m.limit))
		}
	}
	if s.Alerts.Traffic {
		np, err := p.Store.NodeProbe(ctx, n.ID)
		if err == nil && np.LimitBytes > 0 {
			pct := np.Billed() * 100 / np.LimitBytes
			for _, th := range []int64{80, 100} {
				if pct >= th {
					if fire, _ := p.Store.AlertOnce(ctx, n.ID, fmt.Sprintf("traffic%d", th), 30*24*time.Hour, at); fire {
						p.notify(ctx, n, "traffic", fmt.Sprintf("📶 %s: monthly traffic at %d%% (%s of %s)", n.Name, pct, gb(np.Billed()), gb(np.LimitBytes)))
					}
				}
			}
		}
	}
}

// CheckOffline raises one notice per node that stopped beating.
func (p *Probe) CheckOffline(ctx context.Context, at time.Time) {
	s := p.Settings(ctx)
	if !s.Enabled {
		return
	}
	grace := time.Duration(s.Alerts.OfflineSeconds) * time.Second
	nodes, err := p.Store.ListNodes(ctx)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if !n.Paired || n.LastSeenAt == nil {
			continue
		}
		last := *n.LastSeenAt
		if l, ok := p.Live(n.ID); ok && l.At.After(last) {
			last = l.At
		}
		if at.Sub(last) <= grace {
			continue
		}
		if fire, _ := p.Store.AlertOnce(ctx, n.ID, "offline", 24*time.Hour, at); fire {
			p.notify(ctx, n, "offline", fmt.Sprintf("🔴 %s is offline (last seen %s ago)", n.Name, at.Sub(last).Round(time.Minute)))
		}
	}
}

func (p *Probe) notify(ctx context.Context, n *domain.Node, kind, text string) {
	if p.Notify == nil {
		return
	}
	p.Notify.Admin(ctx, text)
	p.Notify.Event(ctx, webhook.NodeAlert, map[string]any{"node_id": n.ID, "node": n.Name, "kind": kind, "message": text})
}

// Live returns the latest beat of a node.
func (p *Probe) Live(nodeID int64) (*Live, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.live[nodeID]
	if !ok {
		return nil, false
	}
	cp := *l
	cp.Ring = append([]Sample(nil), l.Ring...)
	return &cp, true
}

// Recent returns the in-memory ring since `since`.
func (p *Probe) Recent(nodeID int64, since time.Time) []Sample {
	l, ok := p.Live(nodeID)
	if !ok {
		return []Sample{}
	}
	out := []Sample{}
	for _, s := range l.Ring {
		if s.At >= since.Unix() {
			out = append(out, s)
		}
	}
	return out
}

// Traffic period rollover happens inside RecordBeat; nothing periodic is
// needed beyond pruning, which jobs call.

func gb(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/(1<<30)) }
