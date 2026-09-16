package admin

import (
	"net/http"
	"strconv"
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
