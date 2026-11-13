package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
)

func (p *Probe) insightAlerts(ctx context.Context, n *domain.Node, at time.Time) {
	states, e := p.Store.NetworkAlertStates(ctx, n.ID)
	if e != nil {
		return
	}
	tasks, e := p.Store.ListPingTasks(ctx)
	if e != nil {
		return
	}
	plan, e := p.Store.NodeProbePlanView(ctx, n.ID)
	if e != nil {
		return
	}
	policy, e := p.Store.NetworkAlertPolicy(ctx, n.ID)
	if e != nil {
		return
	}
	active := map[string]bool{}
	for _, v := range states {
		kind := "network_latency:" + v.Name + ":" + v.Family + ":" + v.Key[:8]
		lossKind := "network_loss:" + v.Name + ":" + v.Family + ":" + v.Key[:8]
		current := v.TaskID == 0 && plan.Effective.Enabled && v.PlanAt == plan.Effective.ChangedAt
		if v.TaskID == 0 {
			current = current && store.CurrentCarrierAlert(plan.Effective, v.Name, v.Series, v.Family, v.Last)
		} else {
			generation, e := p.Store.CurrentTaskAlert(ctx, v.TaskID, v.PlanAt)
			current = e == nil && generation && store.CurrentTaskSample(tasks, n.ID, v.TaskID, v.Name, v.Series, v.Family)
		}
		if policy.Mode == "off" || !current {
			continue
		}
		active[kind] = true
		active[lossKind] = true
		if (!v.Ready && !v.LossReady) || at.Unix()-v.Last > int64(max(120, 3*v.Interval)) || at.Unix() < v.Last {
			continue
		}
		if v.Ready {
			p.incident(ctx, n, store.IncidentCheck{SourceKind: "network", SourceKey: v.Key, SourceAt: v.Last, Kind: kind, Active: v.LatencyAlert, Value: v.Average, Threshold: v.Threshold}, at, fmt.Sprintf("⚠️ %s: %s %s latency %.1f ms (limit %.1f ms)", notify.Escape(n.Name), notify.Escape(v.Name), v.Family, v.Average, v.Threshold))
		}
		if v.LossReady {
			p.incident(ctx, n, store.IncidentCheck{SourceKind: "network", SourceKey: v.Key, SourceAt: v.Last, Kind: lossKind, Active: v.LossAlert, Value: v.Loss, Threshold: policy.LossPct}, at, fmt.Sprintf("⚠️ %s: %s %s packet loss %.1f%%", notify.Escape(n.Name), notify.Escape(v.Name), v.Family, v.Loss))
		}
	}
	// Editing/removing a task ends its incident without claiming recovery.
	incidents, e := p.Store.MonitorIncidents(ctx, n.ID, 0, "open")
	if e == nil {
		for _, i := range incidents {
			if strings.HasPrefix(i.Kind, "network_") && !active[i.Kind] {
				p.incident(ctx, n, store.IncidentCheck{Kind: i.Kind, Resolution: "disabled"}, at, "")
			}
		}
	}
	checks, e := p.Store.RouteAlertChecks(ctx, n.ID, at)
	if e != nil {
		return
	}
	for _, c := range checks {
		p.incident(ctx, n, c, at, fmt.Sprintf("⚠️ %s: %s no longer matches the expected return route; see route timeline", notify.Escape(n.Name), c.Kind))
	}
}
