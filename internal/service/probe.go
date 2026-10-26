package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
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
	exchangeRateClient *http.Client // internal test transport; production is fixed HTTPS
	Store              *store.Store
	Notify             *notify.Notifier
	Log                *slog.Logger
	// AlertWindow batches the operator notices raised within it into one
	// Telegram message (a panel-side blip takes every node offline at
	// once); 0 = 30 s, negative = send each one immediately. Webhook
	// events are always emitted per alert.
	AlertWindow time.Duration

	started    time.Time // first CheckOffline; see the grace period there
	alertMu    sync.Mutex
	pending    []probeNotice
	alertTimer *time.Timer
	adminSend  func(ctx context.Context, text string) // tests

	monitorMu    sync.Mutex
	monitorEpoch string
	mu           sync.Mutex
	live         map[int64]*Live
	settings     store.ProbeSettings
	fetched      time.Time
	gen          uint64 // bumped on every settings change; the snapshot cache keys on it
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
	Valid   *spec.MetricValidity `json:"valid,omitempty"`
	At      int64                `json:"t"`
	CPU     float64              `json:"cpu"`
	MemPct  float64              `json:"mem"`
	NetUp   uint64               `json:"up"`
	NetDown uint64               `json:"down"`
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
	if err := p.Store.GetSetting(ctx, store.SettingProbe, &s); err != nil {
		return p.settings // keep what we had rather than "probe is off"
	}
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

// Reset discards the old installation's node samples and pending alerts.
// The caller holds Store.Accounts exclusively against requests and jobs.
func (p *Probe) Reset() {
	p.mu.Lock()
	p.live = nil
	p.monitorEpoch = ""
	p.settings = store.ProbeSettings{}
	p.fetched, p.started = time.Time{}, time.Time{}
	p.gen++
	p.mu.Unlock()
	p.discardPendingAlerts()
}

// AgentConfig is what a node receives in its state.
func (p *Probe) AgentConfig(ctx context.Context, nodeID int64) *spec.Probe {
	s := p.Settings(ctx)
	var resources *spec.ResourceOptions
	if np, err := p.Store.NodeProbe(ctx, nodeID); err == nil {
		if np.Resources.GPU || len(np.Resources.IncludeInterfaces)+len(np.Resources.ExcludeInterfaces) > 0 {
			resources = &np.Resources
		}
	}
	if !s.Enabled {
		if resources != nil {
			return &spec.Probe{Resources: resources}
		}
		return nil
	}
	cfg := &spec.Probe{Resources: resources, Enabled: true, BeatSeconds: s.BeatSeconds}
	if s.CarrierPing {
		cfg.CarrierProbe = s.CarrierProbe.Clone()
		// Older agents ignore CarrierProbe. Leave CarrierPing false so they
		// cannot silently report legacy TCP results as ICMP or IPv6 samples.
	}
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
	// Dedicated lines: measure each ingress from its own NIC to the far
	// end (a refused connect still yields the line RTT). Task ids are the
	// negative ingress id so they never collide with panel tasks.
	if ingresses, err := p.Store.IngressesByNode(ctx, nodeID); err == nil && len(ingresses) > 0 {
		inbounds, _ := p.Store.InboundsByNode(ctx, nodeID)
		for _, g := range ingresses {
			if g.BindIP == "" || g.LineIP == "" {
				continue
			}
			port := 0
			for _, ib := range inbounds {
				if ib.IngressID != nil && *ib.IngressID == g.ID {
					port = ib.Port
					break
				}
			}
			if port == 0 {
				port = g.ProbePort()
			}
			cfg.Tasks = append(cfg.Tasks, spec.PingTask{ID: -g.ID, Name: g.Name, Type: "tcp", Target: net.JoinHostPort(g.LineIP, strconv.Itoa(port)), IntervalSeconds: 30, SourceIP: g.BindIP, TCPReachability: true})
		}
	}
	return cfg
}

// Record takes one beat from a node.
func (p *Probe) Record(ctx context.Context, n *domain.Node, version string, host spec.SystemStatus, at time.Time, tokenHash ...string) error {
	s := p.Settings(ctx)
	if !s.Enabled {
		return nil
	}
	p.mu.Lock()
	var err error
	renewed := false
	if len(tokenHash) > 0 {
		renewed, err = p.Store.RecordNodeBeat(ctx, n.ID, tokenHash[0], host, at)
	} else {
		err = p.Store.RecordBeat(ctx, n.ID, host, at)
	}
	if err != nil {
		p.mu.Unlock()
		if errors.Is(err, store.ErrStaleBeat) {
			return nil
		}
		return err
	}
	if renewed {
		p.gen++
		p.fetched = time.Time{}
	}
	if p.live == nil {
		p.live = map[int64]*Live{}
	}
	l := p.live[n.ID]
	if l == nil {
		l = &Live{}
		p.live[n.ID] = l
	}
	host = store.MonitorSnapshot(host, at)
	l.Host, l.At, l.Version = host, at, version
	memPct := 0.0
	if host.MemTotal > 0 {
		memPct = float64(host.MemUsed) * 100 / float64(host.MemTotal)
	}
	l.Ring = append(l.Ring, Sample{Valid: host.Valid, At: at.Unix(), CPU: host.CPUPercent, MemPct: memPct, NetUp: host.NetUp, NetDown: host.NetDown})
	if len(l.Ring) > ringMax {
		l.Ring = l.Ring[len(l.Ring)-ringMax:]
	}
	p.mu.Unlock()
	p.monitorMu.Lock()
	defer p.monitorMu.Unlock()
	s = p.Settings(ctx)
	if !s.Enabled {
		return nil
	}
	if err := p.Store.ObserveAvailability(ctx, n.ID, p.observationEpoch(), at, at, time.Duration(s.Alerts.OfflineSeconds)*time.Second); err != nil {
		return err
	}
	p.incident(ctx, n, store.IncidentCheck{Kind: "offline", Threshold: float64(s.Alerts.OfflineSeconds)}, at, "")
	p.thresholds(ctx, n, s, &host, at)
	return nil
}

func (p *Probe) observationEpoch() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.monitorEpoch == "" {
		p.monitorEpoch = rand.Text()
	}
	return p.monitorEpoch
}

func (p *Probe) incident(ctx context.Context, n *domain.Node, c store.IncidentCheck, at time.Time, text string) {
	notice, err := p.Store.CheckIncident(ctx, n.ID, c, at)
	if err != nil {
		if p.Log != nil {
			p.Log.Error("monitor incident", "node", n.ID, "kind", c.Kind, "err", err)
		}
		return
	}
	if notice == nil {
		return
	}
	kind := c.Kind
	if notice.Recovered {
		kind = "recovered"
		if c.Kind != "offline" {
			kind = c.Kind + "_recovered"
		}
		text = fmt.Sprintf("✅ %s: %s recovered", notify.Escape(n.Name), c.Kind)
	} else if strings.HasPrefix(kind, "traffic") {
		kind = "traffic"
	}
	p.notify(ctx, n, kind, text, notice)
}

func (p *Probe) thresholds(ctx context.Context, n *domain.Node, s store.ProbeSettings, host *spec.SystemStatus, at time.Time) {
	window := time.Duration(s.Alerts.WindowMinutes) * time.Minute
	for _, m := range []struct {
		kind  string
		limit int
	}{{"cpu", s.Alerts.CPUPct}, {"mem", s.Alerts.MemPct}, {"disk", s.Alerts.DiskPct}} {
		c := store.IncidentCheck{Kind: m.kind, Threshold: float64(m.limit)}
		if m.limit <= 0 {
			c.Resolution = "disabled"
			p.incident(ctx, n, c, at, "")
			continue
		}
		if host == nil {
			continue
		}
		valid := host.Validity()
		if (m.kind == "cpu" && !valid.CPU) || (m.kind == "mem" && !valid.Memory) || (m.kind == "disk" && !valid.Disk) {
			continue
		}
		avg, ok := p.Store.SustainedAverage(ctx, n.ID, m.kind, window, at)
		if !ok {
			continue
		}
		c.Value = avg
		c.Active = avg >= float64(m.limit)
		p.incident(ctx, n, c, at, fmt.Sprintf("⚠️ %s: %s at %.0f%% over the last %d min (limit %d%%)", notify.Escape(n.Name), strings.ToUpper(m.kind), avg, s.Alerts.WindowMinutes, m.limit))
	}
	np, err := p.Store.NodeProbe(ctx, n.ID)
	if err != nil {
		return
	}
	for _, th := range []int64{80, 100} {
		c := store.IncidentCheck{Kind: fmt.Sprintf("traffic%d", th), Threshold: float64(th)}
		if !s.Alerts.Traffic || np.LimitBytes <= 0 {
			c.Resolution = "disabled"
			p.incident(ctx, n, c, at, "")
			continue
		}
		c.Value = float64(np.Billed()) * 100 / float64(np.LimitBytes)
		c.Active = c.Value >= float64(th)
		p.incident(ctx, n, c, at, fmt.Sprintf("📶 %s: monthly traffic at %.0f%% (%s of %s)", notify.Escape(n.Name), c.Value, gb(np.Billed()), gb(np.LimitBytes)))
	}
}

// CheckOffline evaluates persisted incidents as well as liveness. A process
// restart grants a new offline grace; pending incidents remain in the database.
func (p *Probe) CheckOffline(ctx context.Context, at time.Time) {
	p.monitorMu.Lock()
	defer p.monitorMu.Unlock()
	s := p.Settings(ctx)
	if !s.Enabled {
		if err := p.Store.DisableMonitoring(ctx, at); err != nil && p.Log != nil {
			p.Log.Error("disable monitoring", "err", err)
		}
		return
	}
	grace := time.Duration(s.Alerts.OfflineSeconds) * time.Second
	p.mu.Lock()
	if p.started.IsZero() {
		p.started = at
	}
	started := p.started
	p.mu.Unlock()
	if at.Sub(started) <= grace {
		return
	}
	nodes, err := p.Store.ListNodes(ctx)
	if err != nil {
		return
	}
	epoch := p.observationEpoch()
	for _, n := range nodes {
		if !n.Paired || n.LastSeenAt == nil {
			continue
		}
		last := *n.LastSeenAt
		live, ok := p.Live(n.ID)
		if ok && live.At.After(last) {
			last = live.At
		}
		if err = p.Store.ObserveAvailability(ctx, n.ID, epoch, at, last, grace); err != nil {
			if p.Log != nil {
				p.Log.Error("availability", "node", n.ID, "err", err)
			}
			continue
		}
		offline := at.Sub(last) > grace
		p.incident(ctx, n, store.IncidentCheck{Kind: "offline", Active: offline, Value: at.Sub(last).Seconds(), Threshold: grace.Seconds()}, at, fmt.Sprintf("🔴 %s is offline (last seen %s ago)", notify.Escape(n.Name), at.Sub(last).Round(time.Minute)))
		var host *spec.SystemStatus
		if !offline && ok && at.Sub(live.At) <= max(90*time.Second, time.Duration(s.BeatSeconds*2)*time.Second) {
			host = &live.Host
		}
		p.thresholds(ctx, n, s, host, at)
	}
}

// MonitoringDisabled is called immediately when the collection switch is saved.
func (p *Probe) MonitoringDisabled(ctx context.Context, at time.Time) error {
	p.monitorMu.Lock()
	defer p.monitorMu.Unlock()
	p.mu.Lock()
	p.started = time.Time{}
	p.monitorEpoch = ""
	p.mu.Unlock()
	p.discardPendingAlerts()
	return p.Store.DisableMonitoring(ctx, at)
}

func (p *Probe) notify(ctx context.Context, n *domain.Node, kind, text string, incidents ...*store.IncidentNotice) {
	if p.Notify == nil {
		return
	}
	data := map[string]any{"node_id": n.ID, "node": n.Name, "kind": kind, "message": text}
	if len(incidents) > 0 {
		data["incident_id"] = incidents[0].ID
		data["alert_kind"] = incidents[0].Kind
		data["recovered"] = incidents[0].Recovered
	}
	p.Notify.Event(ctx, webhook.NodeAlert, data)
	p.queueAdmin(text, n.ID)
}

func (p *Probe) discardPendingAlerts() {
	p.alertMu.Lock()
	defer p.alertMu.Unlock()
	if p.alertTimer != nil {
		p.alertTimer.Stop()
		p.alertTimer = nil
	}
	p.pending = nil
}

// queueAdmin collects operator notices for AlertWindow and sends them as
// one message.
type probeNotice struct {
	Text   string
	NodeID int64
}

func (p *Probe) queueAdmin(text string, nodeID int64) {
	send := p.adminSend
	if send == nil {
		send = p.Notify.Admin
	}
	window := p.AlertWindow
	if window == 0 {
		window = 30 * time.Second
	}
	if window < 0 {
		send(context.Background(), text)
		return
	}
	p.alertMu.Lock()
	defer p.alertMu.Unlock()
	p.pending = append(p.pending, probeNotice{Text: text, NodeID: nodeID})
	if p.alertTimer == nil {
		p.alertTimer = time.AfterFunc(window, func() {
			p.alertMu.Lock()
			pending := p.pending
			p.pending, p.alertTimer = nil, nil
			p.alertMu.Unlock()
			lines := []string{}
			if p.Store != nil && !p.Settings(context.Background()).Enabled {
				return
			}
			for _, n := range pending {
				if p.Store != nil {
					muted, err := p.Store.MonitorMuted(context.Background(), n.NodeID, time.Now())
					if err != nil || muted {
						continue
					}
				}
				lines = append(lines, n.Text)
			}
			switch len(lines) {
			case 0:
			case 1:
				send(context.Background(), lines[0])
			default:
				send(context.Background(), batchText(lines))
			}
		})
	}
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

// tgLimit is Telegram's per-message ceiling; a longer message is rejected
// outright, so a fleet-wide alert has to be cut.
const tgLimit = 4096

// batchText joins the notices into one message no longer than Telegram
// allows, naming how many did not fit.
func batchText(lines []string) string {
	head := fmt.Sprintf("📣 %d node alerts", len(lines))
	var b strings.Builder
	b.WriteString(head)
	for i, l := range lines {
		tail := ""
		if left := len(lines) - i; left > 1 {
			tail = fmt.Sprintf("\n… and %d more", left)
		}
		if b.Len()+1+len(l)+len(tail) > tgLimit {
			b.WriteString(fmt.Sprintf("\n… and %d more", len(lines)-i))
			return b.String()
		}
		b.WriteString("\n")
		b.WriteString(l)
	}
	return b.String()
}
