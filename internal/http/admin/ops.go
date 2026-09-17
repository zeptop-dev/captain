package admin

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/service"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerOps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/tickets", h.requireAdmin(h.listTickets))
	mux.HandleFunc("GET /api/admin/tickets/{id}", h.requireAdmin(h.getTicket))
	mux.HandleFunc("POST /api/admin/tickets/{id}/reply", h.requireAdmin(h.replyTicket))
	mux.HandleFunc("POST /api/admin/tickets/{id}/status", h.requireAdmin(h.ticketStatus))
	mux.HandleFunc("GET /api/admin/gifts", h.requireAdmin(h.listGifts))
	mux.HandleFunc("GET /api/admin/gifts/batches", h.requireAdmin(h.giftBatches))
	mux.HandleFunc("POST /api/admin/gifts", h.requireAdmin(h.createGifts))
	mux.HandleFunc("DELETE /api/admin/gifts/batches/{batch}", h.requireAdmin(h.deleteGiftBatch))
	mux.HandleFunc("GET /api/admin/articles", h.requireAdmin(h.listArticles))
	mux.HandleFunc("POST /api/admin/articles", h.requireAdmin(h.createArticle))
	mux.HandleFunc("PATCH /api/admin/articles/{id}", h.requireAdmin(h.updateArticle))
	mux.HandleFunc("DELETE /api/admin/articles/{id}", h.requireAdmin(h.deleteArticle))
	mux.HandleFunc("GET /api/admin/settings/security", h.requireAdmin(h.getSecurity))
	mux.HandleFunc("PUT /api/admin/settings/security", h.requireAdmin(h.putSecurity))
	mux.HandleFunc("GET /api/admin/settings/clients", h.requireAdmin(h.getClients))
	mux.HandleFunc("PUT /api/admin/settings/clients", h.requireAdmin(h.putClients))
	mux.HandleFunc("GET /api/admin/settings/telegram", h.requireAdmin(h.getTelegram))
	mux.HandleFunc("PUT /api/admin/settings/telegram", h.requireAdmin(h.putTelegram))
	mux.HandleFunc("POST /api/admin/settings/telegram/test", h.requireAdmin(h.testTelegram))
	mux.HandleFunc("GET /api/admin/admins", h.requireAdmin(h.listStaff))
	mux.HandleFunc("POST /api/admin/admins", h.requireAdmin(h.createStaff))
	mux.HandleFunc("PATCH /api/admin/admins/{id}", h.requireAdmin(h.updateStaff))
	mux.HandleFunc("DELETE /api/admin/admins/{id}", h.requireAdmin(h.deleteStaff))
	mux.HandleFunc("GET /api/admin/settings/webhooks", h.requireAdmin(h.getWebhooks))
	mux.HandleFunc("PUT /api/admin/settings/webhooks", h.requireAdmin(h.putWebhooks))
	mux.HandleFunc("POST /api/admin/settings/webhooks/test", h.requireAdmin(h.testWebhook))
	mux.HandleFunc("GET /api/admin/settings/connlog", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		getSetting[store.ConnLogSettings](h, w, r, store.SettingConnLog, func(v *store.ConnLogSettings) { v.RetentionDays = v.Days() })
	}))
	mux.HandleFunc("PUT /api/admin/settings/connlog", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		putSetting[store.ConnLogSettings](h, w, r, store.SettingConnLog, func(_ context.Context, v *store.ConnLogSettings) string {
			if v.RetentionDays < 1 || v.RetentionDays > 365 {
				return "retention_days must be 1-365"
			}
			return ""
		})
		if h.State != nil {
			h.State.Invalidate()
		}
	}))
	mux.HandleFunc("GET /api/admin/users/{id}/connections", h.requireAdmin(h.userConnections))
	mux.HandleFunc("GET /api/admin/audit-rules", h.requireAdmin(h.getAuditRules))
	mux.HandleFunc("GET /api/admin/settings/dynlimit", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		getSetting[store.DynLimitSettings](h, w, r, store.SettingDynLimit, func(v *store.DynLimitSettings) { *v = v.Defaults() })
	}))
	mux.HandleFunc("PUT /api/admin/settings/dynlimit", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		putSetting[store.DynLimitSettings](h, w, r, store.SettingDynLimit, func(_ context.Context, v *store.DynLimitSettings) string {
			*v = v.Defaults()
			if v.TriggerSeconds < 60 || v.LimitSeconds < 60 {
				return "trigger_seconds and limit_seconds must be at least 60"
			}
			if err := service.ValidateWindows(v.Windows); err != nil {
				return err.Error()
			}
			return ""
		})
		if h.Dyn != nil {
			h.Dyn.Invalidate()
		}
	}))
	mux.HandleFunc("DELETE /api/admin/users/{id}/dyn-limit", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		id, okID := pathID(r)
		if !okID {
			fail(w, http.StatusBadRequest, "bad id")
			return
		}
		if err := h.Store.DeleteDynLimit(r.Context(), id); err != nil {
			serverErr(w, err)
			return
		}
		if h.State != nil {
			h.State.Invalidate()
		}
		ok(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("PUT /api/admin/audit-rules", h.requireAdmin(h.putAuditRules))
	mux.HandleFunc("GET /api/admin/audit-log", h.requireAdmin(h.auditLog))
	mux.HandleFunc("GET /api/admin/settings/audit", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		getSetting[store.AuditSettings](h, w, r, store.SettingAudit, func(v *store.AuditSettings) {
			if v.WindowHours <= 0 {
				v.WindowHours = 24
			}
		})
	}))
	mux.HandleFunc("PUT /api/admin/settings/audit", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		putSetting[store.AuditSettings](h, w, r, store.SettingAudit, func(_ context.Context, v *store.AuditSettings) string {
			if v.AutoBanHits < 0 || v.WindowHours < 0 || v.WindowHours > 720 {
				return "auto_ban_hits must be >= 0 and window_hours 1-720"
			}
			return ""
		})
	}))
	mux.HandleFunc("GET /api/admin/settings/komari", h.requireAdmin(h.getKomari))
	mux.HandleFunc("PUT /api/admin/settings/komari", h.requireAdmin(h.putKomari))
	mux.HandleFunc("GET /api/admin/settings/probe", h.requireAdmin(h.getProbe))
	mux.HandleFunc("PUT /api/admin/settings/probe", h.requireAdmin(h.putProbe))
	mux.HandleFunc("GET /api/admin/ping-tasks", h.requireAdmin(h.listPingTasks))
	mux.HandleFunc("POST /api/admin/ping-tasks", h.requireAdmin(h.savePingTask))
	mux.HandleFunc("PATCH /api/admin/ping-tasks/{id}", h.requireAdmin(h.savePingTask))
	mux.HandleFunc("DELETE /api/admin/ping-tasks/{id}", h.requireAdmin(h.deletePingTask))
	mux.HandleFunc("GET /api/admin/nodes/{id}/probe", h.requireAdmin(h.getNodeProbe))
	mux.HandleFunc("PUT /api/admin/nodes/{id}/probe", h.requireAdmin(h.putNodeProbe))
	mux.HandleFunc("POST /api/admin/nodes/{id}/probe/reset-traffic", h.requireAdmin(h.resetNodeTraffic))
	mux.HandleFunc("POST /api/admin/users/{id}/subscription", h.requireAdmin(h.adjustSubscription))
	mux.HandleFunc("DELETE /api/admin/users/{id}/subscriptions/{sid}", h.requireAdmin(h.cancelQueuedSub))
	mux.HandleFunc("GET /api/admin/renewals", h.requireAdmin(h.renewals))
	mux.HandleFunc("GET /api/admin/tokens", h.requireAdmin(h.listTokens))
	mux.HandleFunc("POST /api/admin/tokens", h.requireAdmin(h.createToken))
	mux.HandleFunc("DELETE /api/admin/tokens/{id}", h.requireAdmin(h.deleteToken))
	mux.HandleFunc("POST /api/admin/2fa/setup", h.requireAdmin(h.totpSetup))
	mux.HandleFunc("POST /api/admin/2fa/enable", h.requireAdmin(h.totpEnable))
	mux.HandleFunc("POST /api/admin/2fa/disable", h.requireAdmin(h.totpDisable))
	mux.HandleFunc("GET /api/admin/users/{id}/entries", h.requireAdmin(h.userEntries))
	mux.HandleFunc("PUT /api/admin/users/{id}/entries", h.requireAdmin(h.putUserEntries))
	mux.HandleFunc("GET /api/admin/users/{id}/links", h.requireAdmin(h.listSubLinks))
	mux.HandleFunc("POST /api/admin/users/{id}/links", h.requireAdmin(h.createTempLink))
	mux.HandleFunc("DELETE /api/admin/users/{id}/links/{lid}", h.requireAdmin(h.deleteSubLink))
	mux.HandleFunc("GET /api/admin/settings/trial", h.requireAdmin(h.getTrial))
	mux.HandleFunc("PUT /api/admin/settings/trial", h.requireAdmin(h.putTrial))
}

func idOf(r *http.Request) int64 {
	id, _ := pathID(r)
	return id
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil && v >= 0 {
		return v
	}
	return def
}

// ---- tickets --------------------------------------------------------------------

// ---- gift codes ------------------------------------------------------------------

// ---- articles ----------------------------------------------------------------------

// ---- settings: clients, telegram, trial -----------------------------------------------

// ---- surplus + withdrawals ----------------------------------------------------------

// ---- staff accounts -------------------------------------------------------------------

// ---- webhooks ---------------------------------------------------------------------------

// ---- probe / monitoring ---------------------------------------------------------------

// ---- subscription adjustments / renewal view ----------------------------------------------

// ---- personal API tokens -------------------------------------------------------------

// ---- two-factor for staff -------------------------------------------------------------

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- temporary subscription links ----------------------------------------------------

// ---- komari -------------------------------------------------------------------------

// userConnections lists a user's recent connections (the connection log).
func (h *handlers) userConnections(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.Store.UserConnections(r.Context(), id, limit)
	if err != nil {
		serverErr(w, err)
		return
	}
	if rows == nil {
		rows = []store.ConnRow{}
	}
	ok(w, rows)
}

// getAuditRules lists the rules with their hit counts of the last 7 days.
func (h *handlers) getAuditRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.Store.AuditRules(r.Context(), false)
	if err != nil {
		serverErr(w, err)
		return
	}
	counts, _ := h.Store.RuleHitCounts(r.Context(), time.Now().AddDate(0, 0, -7))
	out := make([]map[string]any, 0, len(rules))
	for _, x := range rules {
		out = append(out, map[string]any{"id": x.ID, "name": x.Name, "match": x.Match, "action": x.Action, "enabled": x.Enabled, "hits": counts[x.ID]})
	}
	ok(w, out)
}

// putAuditRules replaces the list; nodes pick the change up on their next
// state poll.
func (h *handlers) putAuditRules(w http.ResponseWriter, r *http.Request) {
	var in []store.AuditRule
	if !readJSON(w, r, &in) {
		return
	}
	for i := range in {
		in[i].Name = strings.TrimSpace(in[i].Name)
		if in[i].Name == "" {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %d: name is required", i+1))
			return
		}
		if in[i].Action != "block" && in[i].Action != "log" {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %q: action must be block or log", in[i].Name))
			return
		}
		var clean []string
		for _, mt := range in[i].Match {
			mt = strings.TrimSpace(mt)
			if mt == "" {
				continue
			}
			if strings.ContainsAny(mt, "\r\n\"") {
				fail(w, http.StatusBadRequest, fmt.Sprintf("rule %q: bad match %q", in[i].Name, mt))
				return
			}
			if k, v, okc := strings.Cut(mt, ":"); okc && k == "regexp" {
				if _, err := regexp.Compile(v); err != nil {
					fail(w, http.StatusBadRequest, fmt.Sprintf("rule %q: bad regexp: %v", in[i].Name, err))
					return
				}
			}
			clean = append(clean, mt)
		}
		if len(clean) == 0 {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %q: at least one match is required", in[i].Name))
			return
		}
		in[i].Match = clean
	}
	rules, err := h.Store.ReplaceAuditRules(r.Context(), in)
	if err != nil {
		serverErr(w, err)
		return
	}
	if h.State != nil {
		h.State.Invalidate()
	}
	ok(w, rules)
}

// auditLog lists recent hits, optionally for one user.
func (h *handlers) auditLog(w http.ResponseWriter, r *http.Request) {
	userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.Store.AuditHits(r.Context(), userID, limit)
	if err != nil {
		serverErr(w, err)
		return
	}
	if rows == nil {
		rows = []store.AuditHit{}
	}
	ok(w, rows)
}
