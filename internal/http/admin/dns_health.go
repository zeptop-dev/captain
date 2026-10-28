package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerDNSHealth(mux *http.ServeMux) {
	// These handlers own account locks around provider reads and mutations.
	mux.HandleFunc("GET /api/admin/nodes/{id}/dns-sync", h.nodeDNSSync)
	mux.HandleFunc("POST /api/admin/nodes/{id}/dns-sync", h.nodeDNSSync)
	mux.HandleFunc("GET /api/admin/nodes/{id}/dns-health", h.requireAdmin(h.getDNSHealth))
	mux.HandleFunc("POST /api/admin/nodes/{id}/dns-health/check", h.requireAdmin(h.checkDNSHealth))
	mux.HandleFunc("GET /api/admin/monitoring/dns", h.requireAdmin(h.listDNSHealth))
	mux.HandleFunc("GET /api/admin/settings/dns/history", h.requireAdmin(h.dnsHistory))
}
func dnsHealthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "node not found")
	case errors.Is(err, service.ErrDNSBusy):
		fail(w, 429, "DNS check busy; retry shortly")
	case errors.Is(err, service.ErrDNSSyncDisabled):
		fail(w, 409, "DNS synchronization is unavailable; review the current plan")
	case errors.Is(err, service.ErrDNSConfigChanged):
		fail(w, 409, "DNS configuration changed; retry check")
	default:
		serverErr(w, err)
	}
}
func (h *handlers) getDNSHealth(w http.ResponseWriter, r *http.Request) {
	v, err := h.DNSHealth.Snapshot(r.Context(), idOf(r))
	if err != nil {
		dnsHealthError(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) checkDNSHealth(w http.ResponseWriter, r *http.Request) {
	v, err := h.DNSHealth.Check(r.Context(), idOf(r))
	if err != nil {
		dnsHealthError(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) listDNSHealth(w http.ResponseWriter, r *http.Request) {
	v, err := h.DNSHealth.Snapshots(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) dnsHistory(w http.ResponseWriter, r *http.Request) {
	parse := func(key string) (int64, error) {
		s := r.URL.Query().Get(key)
		if s == "" {
			return 0, nil
		}
		return strconv.ParseInt(s, 10, 64)
	}
	node, err := parse("node_id")
	if err != nil || node < 0 {
		fail(w, 400, "invalid node_id")
		return
	}
	before, err := parse("before")
	if err != nil || before < 0 {
		fail(w, 400, "invalid cursor")
		return
	}
	rows, err := h.Store.DNSChanges(r.Context(), node, before)
	if err != nil {
		serverErr(w, err)
		return
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	ok(w, map[string]any{"items": rows, "more": more})
}

func (h *handlers) checkDNS(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "dns", Status: "skip"}
	if h.DNSHealth == nil {
		return []selfCheck{c}
	}
	rows, err := h.DNSHealth.Snapshots(ctx)
	if err != nil {
		c.Status, c.Code = "warn", "unknown"
		return []selfCheck{c}
	}
	bad, unknown := 0, 0
	for _, row := range rows {
		if row.Stale || row.CheckedAt == 0 {
			unknown++
			continue
		}
		for _, t := range row.Targets {
			switch t.Status {
			case "mismatch", "missing", "conflict", "invalid":
				bad++
			case "unknown", "inconsistent":
				unknown++
			}
		}
	}
	switch {
	case bad > 0:
		c.Status, c.Code = "warn", "problem"
		c.Args = map[string]any{"count": bad}
	case unknown > 0:
		c.Status, c.Code = "warn", "pending"
		c.Args = map[string]any{"count": unknown}
	case len(rows) > 0:
		c.Status = "ok"
	}
	return []selfCheck{c}
}
