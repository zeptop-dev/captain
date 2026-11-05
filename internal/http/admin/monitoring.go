package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) monitoring(w http.ResponseWriter, r *http.Request) {
	grace := 180 * time.Second
	fresh := 90 * time.Second
	if h.Probe != nil {
		settings := h.Probe.Settings(r.Context())
		grace = time.Duration(settings.Alerts.OfflineSeconds) * time.Second
		if settings.Enabled {
			fresh = max(fresh, time.Duration(settings.BeatSeconds)*2*time.Second)
		}
	}
	nodes, err := h.Store.MonitorNodes(r.Context(), time.Now(), grace)
	if err != nil {
		serverErr(w, err)
		return
	}
	for i := range nodes {
		nodes[i].Stale = nodes[i].SampledAt == 0 || time.Since(time.Unix(nodes[i].SampledAt, 0)) > fresh
		if nodes[i].Host != nil {
			nodes[i].Host.Resources = nil
		}
	}
	ok(w, nodes)
}

func (h *handlers) resourceHistory(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Store.NodeByID(r.Context(), idOf(r)); err != nil {
		fail(w, 404, "node not found")
		return
	}
	q := r.URL.Query()
	if _, _, _, valid := store.ResourceRange(q.Get("range")); !valid || len(q.Get("series")) > 1024 {
		fail(w, 400, "invalid history query")
		return
	}
	result, err := h.Store.ResourceHistory(r.Context(), idOf(r), q.Get("range"), q.Get("series"), time.Now())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, result)
}

func (h *handlers) setMonitorGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Group string `json:"group"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Group = strings.TrimSpace(in.Group)
	if len(in.Group) > 128 || !spec.PublicLabel(in.Group) {
		fail(w, 400, "invalid monitoring group")
		return
	}
	if _, err := h.Store.NodeByID(r.Context(), idOf(r)); err != nil {
		fail(w, 404, "node not found")
		return
	}
	if err := h.Store.SetMonitorGroup(r.Context(), idOf(r), in.Group); err != nil {
		serverErr(w, err)
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	ok(w, map[string]string{"group": in.Group})
}

// Network history is separate from the public-page gate, so private collection
// remains useful with public visibility disabled.
func (h *handlers) networkQuality(w http.ResponseWriter, r *http.Request) {
	span := r.URL.Query().Get("range")
	res, step, window, valid := store.ResourceRange(span)
	if !valid {
		fail(w, 400, "invalid history range")
		return
	}
	now := time.Now()
	nodes, err := h.Store.MonitorNodes(r.Context(), now, 180*time.Second, idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	if len(nodes) == 0 {
		fail(w, 404, "node not found")
		return
	}
	points, err := h.Store.NodePingStats(r.Context(), idOf(r), res, now.Add(-window), now)
	if err != nil {
		serverErr(w, err)
		return
	}
	tasks, err := h.Store.ListPingTasks(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	paths := map[int64]string{}
	for _, task := range tasks {
		if task.SourceInterface != "" {
			paths[task.ID] = task.SourceInterface
		}
	}
	plan, err := h.Store.NodeProbePlan(r.Context(), idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	var settings store.ProbeSettings
	if err = h.Store.GetSetting(r.Context(), store.SettingProbe, &settings); err != nil && err != store.ErrNotFound {
		serverErr(w, err)
		return
	}
	settings.Normalize()
	effective := store.ResolveNodeProbePlan(settings, plan)
	points = store.FilterCarrierPoints(points, effective)
	current := []spec.PingResult{}
	if nodes[0].Host != nil {
		current = store.FilterCarrierSamples(store.FilterTaskSamples(*nodes[0].Host, tasks, idOf(r)), effective).Pings
	}
	if current == nil {
		current = []spec.PingResult{}
	}
	ok(w, map[string]any{"probe_plan": publicProbePlanSummary(effective), "current": current, "points": points, "task_paths": paths, "from": now.Add(-window).Unix(), "to": now.Unix(), "step": int64(step / time.Second)})
}

// Network quality readers do not necessarily have permission to inspect the
// private plan. Only presentation metadata belongs on this broader endpoint.
func publicProbePlanSummary(plan store.EffectiveProbePlan) map[string]any {
	cfg := plan.Config.Normalize()
	custom := false
	if cfg.Protocol == "icmp" {
		custom = (cfg.Family != "ipv6" && len(cfg.ICMPIPv4) > 0) || (cfg.Family != "ipv4" && len(cfg.ICMPIPv6) > 0)
	} else {
		custom = (cfg.Family != "ipv6" && len(cfg.TCPIPv4) > 0) || (cfg.Family != "ipv4" && len(cfg.TCPIPv6) > 0)
	}
	return map[string]any{"enabled": plan.Enabled, "source": plan.Source, "changed_at": plan.ChangedAt, "custom_targets": custom, "config": map[string]string{"protocol": cfg.Protocol, "family": cfg.Family, "tcp_province": cfg.TCPProvince}}
}
