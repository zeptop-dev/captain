package admin

import (
	"errors"
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
	"strconv"
	"time"
)

func (h *handlers) registerInsights(m *http.ServeMux) {
	m.HandleFunc("GET /api/admin/settings/monitor-storage", h.requireAdmin(h.monitorStorage))
	m.HandleFunc("POST /api/admin/settings/monitor-storage/maintain", h.requireAdmin(h.maintainMonitoring))
	m.HandleFunc("GET /api/admin/settings/nodes/{id}/network-alerts", h.requireAdmin(h.networkAlerts))
	m.HandleFunc("PUT /api/admin/settings/nodes/{id}/network-alerts", h.requireAdmin(h.networkAlerts))
	m.HandleFunc("PUT /api/admin/settings/nodes/{id}/route-watch", h.requireAdmin(h.setRouteWatch))
	m.HandleFunc("POST /api/admin/settings/monitor-incidents/{id}/silence", h.requireAdmin(h.silenceIncident))
	m.HandleFunc("GET /api/admin/settings/infrastructure/analytics", h.requireAdmin(h.infraAnalytics))
	m.HandleFunc("POST /api/admin/settings/infrastructure/expenses", h.requireAdmin(h.addInfraExpense))
	m.HandleFunc("POST /api/admin/settings/infrastructure/expenses/{id}/reverse", h.requireAdmin(h.reverseInfraExpense))
}
func (h *handlers) monitorStorage(w http.ResponseWriter, r *http.Request) {
	v, e := h.Store.MonitorStorage(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) maintainMonitoring(w http.ResponseWriter, r *http.Request) {
	v, e := h.Store.MaintainMonitoring(r.Context())
	if errors.Is(e, store.ErrMaintenanceBusy) {
		fail(w, 409, "maintenance already running")
		return
	}
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) networkAlerts(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "bad id")
		return
	}
	if _, e := h.Store.NodeByID(r.Context(), id); e != nil {
		fail(w, 404, "node not found")
		return
	}
	if r.Method == "PUT" {
		var p store.NetworkAlertPolicy
		if !decode(r, &p) || p.Validate() != nil {
			fail(w, 400, "invalid network alert policy")
			return
		}
		if e := h.Store.SetNetworkAlertPolicy(r.Context(), id, p); e != nil {
			serverErr(w, e)
			return
		}
	}
	p, e := h.Store.NetworkAlertPolicy(r.Context(), id)
	if e != nil {
		serverErr(w, e)
		return
	}
	states, e := h.Store.NetworkAlertStates(r.Context(), id)
	if e != nil {
		serverErr(w, e)
		return
	}
	plan, e := h.Store.NodeProbePlanView(r.Context(), id)
	if e != nil {
		serverErr(w, e)
		return
	}
	tasks, e := h.Store.ListPingTasks(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	for i := range states {
		v := &states[i]
		v.Current = store.CurrentCarrierAlert(plan.Effective, v.Name, v.Series, v.Family, v.Last) && v.PlanAt == plan.Effective.ChangedAt
		if v.TaskID != 0 {
			generation, e := h.Store.CurrentTaskAlert(r.Context(), v.TaskID, v.PlanAt)
			if e != nil {
				serverErr(w, e)
				return
			}
			v.Current = generation && store.CurrentTaskSample(tasks, id, v.TaskID, v.Name, v.Series, v.Family)
		}
	}
	ok(w, map[string]any{"policy": p, "states": states, "now": time.Now().Unix()})
}
func (h *handlers) setRouteWatch(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "bad id")
		return
	}
	var p store.RouteWatchPolicy
	if !decode(r, &p) || p.Validate() != nil {
		fail(w, 400, "invalid route watch")
		return
	}
	if _, e := h.Store.NodeByID(r.Context(), id); e != nil {
		fail(w, 404, "node not found")
		return
	}
	if e := h.Store.SetRouteWatch(r.Context(), id, p); e != nil {
		serverErr(w, e)
		return
	}
	ok(w, p)
}
func (h *handlers) silenceIncident(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	var in struct {
		Seconds int64 `json:"seconds"`
	}
	if !valid || !decode(r, &in) {
		fail(w, 400, "invalid silence")
		return
	}
	if e := h.Store.SilenceIncident(r.Context(), id, in.Seconds, time.Now()); e != nil {
		if errors.Is(e, store.ErrNotFound) {
			fail(w, 404, "incident not found")
		} else if errors.Is(e, store.ErrMonitorWindow) {
			fail(w, 400, "invalid silence duration")
		} else {
			serverErr(w, e)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) infraAnalytics(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().UTC().Format("2006-01")
	}
	var id int64
	if raw := r.URL.Query().Get("asset_id"); raw != "" {
		var e error
		id, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "invalid asset")
			return
		}
	}
	v, e := h.Store.InfraAnalytics(r.Context(), month, id, time.Now())
	if e != nil {
		infraError(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) addInfraExpense(w http.ResponseWriter, r *http.Request) {
	var v store.InfraExpense
	if !decode(r, &v) {
		fail(w, 400, "invalid expense")
		return
	}
	if e := h.Store.AddInfraExpense(r.Context(), &v); e != nil {
		infraError(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) reverseInfraExpense(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	var in struct {
		Reason string `json:"reason"`
	}
	if !valid || !decode(r, &in) {
		fail(w, 400, "invalid reversal")
		return
	}
	if e := h.Store.ReverseInfraExpense(r.Context(), id, in.Reason, time.Now()); e != nil {
		infraError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
