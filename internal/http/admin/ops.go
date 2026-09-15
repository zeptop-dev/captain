package admin

import (
	"context"
	"errors"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/webhook"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/telegram"
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

type ticketMsgView struct {
	ID        int64     `json:"id"`
	FromAdmin bool      `json:"from_admin"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type ticketView struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	Email     string          `json:"email"`
	Subject   string          `json:"subject"`
	Status    string          `json:"status"`
	Priority  string          `json:"priority"`
	Messages  int             `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Thread    []ticketMsgView `json:"thread,omitempty"`
}

func (h *handlers) listTickets(w http.ResponseWriter, r *http.Request) {
	limit, offset := queryInt(r, "limit", 50), queryInt(r, "offset", 0)
	rows, total, err := h.Store.ListTickets(r.Context(), 0, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]ticketView, 0, len(rows))
	for _, t := range rows {
		out = append(out, ticketView{ID: t.ID, UserID: t.UserID, Email: t.Email, Subject: t.Subject, Status: t.Status, Priority: t.Priority, Messages: t.Messages, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt})
	}
	ok(w, map[string]any{"items": out, "total": total})
}

func (h *handlers) writeTicket(w http.ResponseWriter, r *http.Request, id int64) {
	t, err := h.Store.TicketByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "ticket not found")
		return
	}
	u, _ := h.Store.UserByID(r.Context(), t.UserID)
	msgs, _ := h.Store.TicketMessages(r.Context(), t.ID)
	v := ticketView{ID: t.ID, UserID: t.UserID, Subject: t.Subject, Status: t.Status, Priority: t.Priority, Messages: len(msgs), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if u != nil {
		v.Email = u.Email
	}
	for _, m := range msgs {
		v.Thread = append(v.Thread, ticketMsgView{ID: m.ID, FromAdmin: m.FromAdmin, Body: m.Body, CreatedAt: m.CreatedAt})
	}
	ok(w, v)
}

func (h *handlers) getTicket(w http.ResponseWriter, r *http.Request) { h.writeTicket(w, r, idOf(r)) }

func (h *handlers) replyTicket(w http.ResponseWriter, r *http.Request) {
	var in struct{ Body string }
	if !decode(r, &in) || strings.TrimSpace(in.Body) == "" {
		fail(w, http.StatusBadRequest, "message is required")
		return
	}
	t, err := h.Store.TicketByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err := h.Store.ReplyTicket(r.Context(), t.ID, true, strings.TrimSpace(in.Body)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u, err := h.Store.UserByID(r.Context(), t.UserID); err == nil {
		h.Notify.User(r.Context(), u.ID, u.Email, "Ticket #"+strconv.FormatInt(t.ID, 10)+": "+t.Subject, strings.TrimSpace(in.Body))
	}
	h.writeTicket(w, r, t.ID)
}

func (h *handlers) ticketStatus(w http.ResponseWriter, r *http.Request) {
	var in struct{ Status string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	switch in.Status {
	case store.TicketOpen, store.TicketReplied, store.TicketClosed:
	default:
		fail(w, http.StatusBadRequest, "status must be open, replied or closed")
		return
	}
	if err := h.Store.SetTicketStatus(r.Context(), idOf(r), in.Status); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeTicket(w, r, idOf(r))
}

// ---- gift codes ------------------------------------------------------------------

type giftView struct {
	ID            int64      `json:"id"`
	Code          string     `json:"code"`
	Batch         string     `json:"batch"`
	Kind          string     `json:"kind"`
	Value         int64      `json:"value"`
	PlanID        *int64     `json:"plan_id"`
	PeriodDays    int        `json:"period_days"`
	ExpiresAt     *time.Time `json:"expires_at"`
	RedeemedBy    *int64     `json:"redeemed_by"`
	RedeemedEmail string     `json:"redeemed_email,omitempty"`
	RedeemedAt    *time.Time `json:"redeemed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (h *handlers) listGifts(w http.ResponseWriter, r *http.Request) {
	rows, total, err := h.Store.ListGiftCodes(r.Context(), r.URL.Query().Get("batch"), queryInt(r, "limit", 100), queryInt(r, "offset", 0))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]giftView, 0, len(rows))
	for _, g := range rows {
		out = append(out, giftView{ID: g.ID, Code: g.Code, Batch: g.Batch, Kind: g.Kind, Value: g.Value, PlanID: g.PlanID, PeriodDays: g.PeriodDays, ExpiresAt: g.ExpiresAt, RedeemedBy: g.RedeemedBy, RedeemedEmail: g.RedeemedEmail, RedeemedAt: g.RedeemedAt, CreatedAt: g.CreatedAt})
	}
	ok(w, map[string]any{"items": out, "total": total})
}

func (h *handlers) giftBatches(w http.ResponseWriter, r *http.Request) {
	bs, err := h.Store.GiftBatches(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(bs))
	for _, b := range bs {
		out = append(out, map[string]any{"batch": b.Batch, "kind": b.Kind, "total": b.Total, "redeemed": b.Redeemed, "created_at": b.CreatedAt})
	}
	ok(w, out)
}

func (h *handlers) createGifts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Batch, Kind, Prefix string
		Count               int
		Value               int64
		PlanID              *int64
		PeriodDays          int
		ExpiresAt           *time.Time
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if in.Count <= 0 || in.Count > 1000 {
		fail(w, http.StatusBadRequest, "count must be 1..1000")
		return
	}
	switch in.Kind {
	case store.GiftBalance, store.GiftTraffic, store.GiftDays:
		if in.Value <= 0 {
			fail(w, http.StatusBadRequest, "value must be positive")
			return
		}
	case store.GiftPlan:
		if in.PlanID == nil {
			fail(w, http.StatusBadRequest, "plan_id is required")
			return
		}
		if _, err := h.Store.PlanByID(r.Context(), *in.PlanID); err != nil {
			fail(w, http.StatusBadRequest, "unknown plan")
			return
		}
	default:
		fail(w, http.StatusBadRequest, "kind must be balance, plan, traffic or days")
		return
	}
	if strings.TrimSpace(in.Batch) == "" {
		in.Batch = time.Now().Format("20060102-150405")
	}
	codes, err := h.Store.CreateGiftCodes(r.Context(), domain.GiftCode{Batch: strings.TrimSpace(in.Batch), Kind: in.Kind, Value: in.Value, PlanID: in.PlanID, PeriodDays: in.PeriodDays, ExpiresAt: in.ExpiresAt}, in.Count, strings.ToUpper(strings.TrimSpace(in.Prefix)))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"batch": in.Batch, "codes": codes})
}

func (h *handlers) deleteGiftBatch(w http.ResponseWriter, r *http.Request) {
	n, err := h.Store.DeleteUnredeemedGiftCodes(r.Context(), r.PathValue("batch"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"deleted": n})
}

// ---- articles ----------------------------------------------------------------------

type articleView struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Body      string    `json:"body"`
	Lang      string    `json:"lang"`
	Sort      int       `json:"sort"`
	Published bool      `json:"published"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toArticleView(a *domain.Article) articleView {
	return articleView{ID: a.ID, Title: a.Title, Category: a.Category, Body: a.Body, Lang: a.Lang, Sort: a.Sort, Published: a.Published, UpdatedAt: a.UpdatedAt}
}

func (h *handlers) listArticles(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListArticles(r.Context(), false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]articleView, 0, len(list))
	for _, a := range list {
		out = append(out, toArticleView(a))
	}
	ok(w, out)
}

func decodeArticle(r *http.Request, a *domain.Article) bool {
	var in struct {
		Title, Category, Body, Lang string
		Sort                        int
		Published                   *bool
	}
	if !decode(r, &in) || strings.TrimSpace(in.Title) == "" {
		return false
	}
	a.Title, a.Category, a.Body, a.Lang, a.Sort = strings.TrimSpace(in.Title), strings.TrimSpace(in.Category), in.Body, in.Lang, in.Sort
	if in.Published != nil {
		a.Published = *in.Published
	}
	return true
}

func (h *handlers) createArticle(w http.ResponseWriter, r *http.Request) {
	a := domain.Article{Published: true}
	if !decodeArticle(r, &a) {
		fail(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := h.Store.CreateArticle(r.Context(), &a); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, toArticleView(&a))
}

func (h *handlers) updateArticle(w http.ResponseWriter, r *http.Request) {
	a, err := h.Store.ArticleByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if !decodeArticle(r, a) {
		fail(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := h.Store.UpdateArticle(r.Context(), a); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, toArticleView(a))
}

func (h *handlers) deleteArticle(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteArticle(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- settings: clients, telegram, trial -----------------------------------------------

func (h *handlers) getSecurity(w http.ResponseWriter, r *http.Request) {
	var v store.SecuritySettings
	_ = h.Store.GetSetting(r.Context(), store.SettingSecurity, &v)
	if v.AdminAllowCIDRs == nil {
		v.AdminAllowCIDRs = []string{}
	}
	ok(w, v)
}

// putSecurity saves the allow-list, refusing one that would lock the
// caller out.
func (h *handlers) putSecurity(w http.ResponseWriter, r *http.Request) {
	var v store.SecuritySettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var clean []string
	for _, c := range v.AdminAllowCIDRs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.Contains(c, "/") {
			if _, _, err := net.ParseCIDR(c); err != nil {
				fail(w, http.StatusBadRequest, "bad CIDR "+c)
				return
			}
		} else if net.ParseIP(c) == nil {
			fail(w, http.StatusBadRequest, "bad address "+c)
			return
		}
		clean = append(clean, c)
	}
	v.AdminAllowCIDRs = clean
	if len(clean) > 0 && !cidrsAllow(clean, ratelimit.ClientIP(r)) {
		fail(w, http.StatusBadRequest, "the allow-list would lock you out: your address "+ratelimit.ClientIP(r)+" is not in it")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSecurity, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.securityMu.Lock()
	h.securityAt = time.Time{}
	h.securityMu.Unlock()
	ok(w, v)
}

// cidrsAllow reports whether ip is inside any entry (bare IPs allowed).
func cidrsAllow(list []string, ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, c := range list {
		if !strings.Contains(c, "/") {
			if ip.Equal(net.ParseIP(c)) {
				return true
			}
			continue
		}
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// adminAllowed applies the console allow-list (cached for 30 s).
func (h *handlers) adminAllowed(ctx context.Context, ip string) bool {
	h.securityMu.Lock()
	if time.Since(h.securityAt) > 30*time.Second {
		var v store.SecuritySettings
		_ = h.Store.GetSetting(ctx, store.SettingSecurity, &v)
		h.securityList, h.securityAt = v.AdminAllowCIDRs, time.Now()
	}
	list := h.securityList
	h.securityMu.Unlock()
	if len(list) == 0 {
		return true
	}
	return cidrsAllow(list, ip)
}

func (h *handlers) getClients(w http.ResponseWriter, r *http.Request) {
	var v store.ClientsSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingClients, &v)
	if v.Items == nil {
		v.Items = []store.ClientItem{}
	}
	ok(w, v)
}

func (h *handlers) putClients(w http.ResponseWriter, r *http.Request) {
	var v store.ClientsSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	items := make([]store.ClientItem, 0, len(v.Items))
	for _, it := range v.Items {
		it.Name, it.URL = strings.TrimSpace(it.Name), strings.TrimSpace(it.URL)
		if it.Name == "" || it.URL == "" {
			continue
		}
		items = append(items, it)
	}
	v.Items = items
	if err := h.Store.SetSetting(r.Context(), store.SettingClients, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

func (h *handlers) getTelegram(w http.ResponseWriter, r *http.Request) {
	var v store.TelegramSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingTelegram, &v)
	has := v.BotToken != ""
	v.BotToken = ""
	ok(w, map[string]any{"settings": v, "has_token": has})
}

func (h *handlers) putTelegram(w http.ResponseWriter, r *http.Request) {
	var v store.TelegramSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur store.TelegramSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingTelegram, &cur)
	v.BotToken = strings.TrimSpace(v.BotToken)
	if v.BotToken == "" {
		v.BotToken, v.BotUsername = cur.BotToken, cur.BotUsername
	}
	if v.BotToken != "" && (v.BotToken != cur.BotToken || v.BotUsername == "") {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		name, err := (&telegram.Client{Token: v.BotToken}).Me(ctx)
		if err != nil {
			fail(w, http.StatusBadRequest, "bot token rejected: "+err.Error())
			return
		}
		v.BotUsername = name
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingTelegram, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Bot != nil {
		h.Bot.Invalidate()
	}
	h.getTelegram(w, r)
}

func (h *handlers) testTelegram(w http.ResponseWriter, r *http.Request) {
	if h.Bot == nil {
		fail(w, http.StatusBadRequest, "telegram not available")
		return
	}
	s := h.Bot.Settings(r.Context())
	if s.BotToken == "" || s.AdminChatID == 0 {
		fail(w, http.StatusBadRequest, "set the bot token and admin chat id first")
		return
	}
	if err := h.Bot.NotifyAdmin(r.Context(), "✅ "+h.SiteName+": Telegram notifications work."); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getTrial(w http.ResponseWriter, r *http.Request) {
	var v store.TrialSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingTrial, &v)
	ok(w, v)
}

func (h *handlers) putTrial(w http.ResponseWriter, r *http.Request) {
	var v store.TrialSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if v.PlanID != 0 {
		if _, err := h.Store.PlanByID(r.Context(), v.PlanID); errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusBadRequest, "unknown plan")
			return
		}
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingTrial, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

// ---- surplus + withdrawals ----------------------------------------------------------

func (h *handlers) getSurplus(w http.ResponseWriter, r *http.Request) {
	var v store.SurplusSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingSurplus, &v)
	ok(w, v)
}

func (h *handlers) putSurplus(w http.ResponseWriter, r *http.Request) {
	var v store.SurplusSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSurplus, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

func (h *handlers) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListWithdrawals(r.Context(), 0, r.URL.Query().Get("status"), queryInt(r, "limit", 200))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.Withdrawal{}
	}
	ok(w, list)
}

func (h *handlers) withdrawalStatus(w http.ResponseWriter, r *http.Request) {
	var in struct{ Status, Note string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if err := h.Store.SetWithdrawalStatus(r.Context(), idOf(r), in.Status, in.Note); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "no pending withdrawal with that id")
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	list, _ := h.Store.ListWithdrawals(r.Context(), 0, "", 1)
	for _, wd := range list {
		if wd.ID == idOf(r) {
			if u, err := h.Store.UserByID(r.Context(), wd.UserID); err == nil {
				h.Notify.User(r.Context(), u.ID, u.Email, "Withdrawal #"+strconv.FormatInt(wd.ID, 10)+" "+in.Status, in.Note)
			}
		}
	}
	ok(w, map[string]string{"status": in.Status})
}

// ---- staff accounts -------------------------------------------------------------------

type staffView struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *handlers) listStaff(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListStaff(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]staffView, 0, len(list))
	for _, u := range list {
		out = append(out, staffView{ID: u.ID, Email: u.Email, Role: u.Role, Status: u.Status, CreatedAt: u.CreatedAt})
	}
	ok(w, out)
}

func (h *handlers) createStaff(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Role string }
	if !decode(r, &in) || !strings.Contains(in.Email, "@") || len(in.Password) < 8 || !domain.ValidStaffRole(in.Role) {
		fail(w, http.StatusBadRequest, "email, a password of 8+ chars and a role (admin, operator, support) are required")
		return
	}
	u, err := NewUser(strings.ToLower(strings.TrimSpace(in.Email)), in.Password, in.Role)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already registered")
		return
	}
	ok(w, staffView{ID: u.ID, Email: u.Email, Role: u.Role, Status: u.Status, CreatedAt: u.CreatedAt})
}

func (h *handlers) updateStaff(w http.ResponseWriter, r *http.Request) {
	var in struct{ Role, Status, Password string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	target, err := h.Store.UserByID(r.Context(), idOf(r))
	if err != nil || !target.IsStaff() {
		fail(w, http.StatusNotFound, "no such staff account")
		return
	}
	if in.Role != "" && !domain.ValidStaffRole(in.Role) {
		fail(w, http.StatusBadRequest, "role must be admin, operator or support")
		return
	}
	// The last active admin keeps its role and stays active.
	demoting := (in.Role != "" && in.Role != domain.RoleAdmin) || (in.Status != "" && in.Status != "active")
	if target.IsAdmin() && demoting {
		if n, _ := h.Store.CountAdmins(r.Context()); n <= 1 {
			fail(w, http.StatusConflict, "cannot demote or disable the last admin")
			return
		}
	}
	if in.Role != "" && in.Role != target.Role {
		if err := h.Store.SetRole(r.Context(), target.ID, in.Role); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	status := target.Status
	if in.Status == "active" || in.Status == "banned" {
		status = in.Status
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < 8 {
			fail(w, http.StatusBadRequest, "password too short")
			return
		}
		if hash, err = auth.HashPassword(in.Password); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := h.Store.UpdateUser(r.Context(), target.ID, status, target.GroupID, hash); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hash != "" {
		_ = h.Store.DeleteUserSessions(r.Context(), target.ID)
	}
	h.listStaff(w, r)
}

func (h *handlers) deleteStaff(w http.ResponseWriter, r *http.Request) {
	target, err := h.Store.UserByID(r.Context(), idOf(r))
	if err != nil || !target.IsStaff() {
		fail(w, http.StatusNotFound, "no such staff account")
		return
	}
	if target.ID == userFrom(r).ID {
		fail(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	if target.IsAdmin() {
		if n, _ := h.Store.CountAdmins(r.Context()); n <= 1 {
			fail(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
	}
	if err := h.Store.DeleteStaff(r.Context(), target.ID); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- webhooks ---------------------------------------------------------------------------

func (h *handlers) getWebhooks(w http.ResponseWriter, r *http.Request) {
	var v webhook.Settings
	_ = h.Store.GetSetting(r.Context(), webhook.SettingKey, &v)
	if v.Endpoints == nil {
		v.Endpoints = []webhook.Endpoint{}
	}
	ok(w, map[string]any{"settings": v, "events": webhook.Events})
}

func (h *handlers) putWebhooks(w http.ResponseWriter, r *http.Request) {
	var v webhook.Settings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	eps := make([]webhook.Endpoint, 0, len(v.Endpoints))
	for _, ep := range v.Endpoints {
		ep.URL = strings.TrimSpace(ep.URL)
		if ep.URL == "" {
			continue
		}
		if !strings.HasPrefix(ep.URL, "http://") && !strings.HasPrefix(ep.URL, "https://") {
			fail(w, http.StatusBadRequest, "endpoint URLs must start with http:// or https://")
			return
		}
		eps = append(eps, ep)
	}
	v.Endpoints = eps
	if err := h.Store.SetSetting(r.Context(), webhook.SettingKey, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Hooks != nil {
		h.Hooks.Invalidate()
	}
	h.getWebhooks(w, r)
}

func (h *handlers) testWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct{ URL, Secret string }
	if !decode(r, &in) || strings.TrimSpace(in.URL) == "" {
		fail(w, http.StatusBadRequest, "url required")
		return
	}
	hub := h.Hooks
	if hub == nil {
		hub = &webhook.Hub{}
	}
	if err := hub.Deliver(webhook.Endpoint{URL: strings.TrimSpace(in.URL), Secret: in.Secret, Enabled: true}, webhook.Test, map[string]any{"site": h.SiteName, "by": userFrom(r).Email}); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- probe / monitoring ---------------------------------------------------------------

func (h *handlers) getProbe(w http.ResponseWriter, r *http.Request) {
	var v store.ProbeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingProbe, &v)
	v.Normalize()
	if v.Hosts == nil {
		v.Hosts = []string{}
	}
	ok(w, v)
}

func (h *handlers) putProbe(w http.ResponseWriter, r *http.Request) {
	var v store.ProbeSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	v.Path = strings.TrimSpace(v.Path)
	if v.Path != "" {
		v.Path = "/" + strings.Trim(v.Path, "/")
		for _, reserved := range []string{"/admin", "/portal", "/sub", "/api", "/assets"} {
			if v.Path == reserved || strings.HasPrefix(v.Path, reserved+"/") {
				fail(w, http.StatusBadRequest, "that path is reserved")
				return
			}
		}
	}
	hosts := []string{}
	for _, hh := range v.Hosts {
		if hh = strings.ToLower(strings.TrimSpace(hh)); hh != "" {
			hosts = append(hosts, hh)
		}
	}
	v.Hosts = hosts
	carriers := []spec.Carrier{}
	seen := map[string]bool{}
	for i, c := range v.Carriers {
		c.Name, c.Addr = strings.TrimSpace(c.Name), strings.TrimSpace(c.Addr)
		if c.Name == "" && c.Addr == "" {
			continue
		}
		if host, port, err := net.SplitHostPort(c.Addr); err != nil || host == "" || port == "" {
			fail(w, http.StatusBadRequest, fmt.Sprintf("carrier %d: address must be host:port", i+1))
			return
		}
		if c.Name == "" || seen[c.Name] {
			fail(w, http.StatusBadRequest, fmt.Sprintf("carrier %d: a unique name is required", i+1))
			return
		}
		seen[c.Name] = true
		carriers = append(carriers, c)
	}
	v.Carriers = carriers
	v.Normalize()
	if err := h.Store.SetSetting(r.Context(), store.SettingProbe, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	ok(w, v)
}

func (h *handlers) listPingTasks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListPingTasks(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

func (h *handlers) savePingTask(w http.ResponseWriter, r *http.Request) {
	var t store.PingTask
	if !decode(r, &t) || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Target) == "" {
		fail(w, http.StatusBadRequest, "name and target are required")
		return
	}
	switch t.Type {
	case "icmp", "tcp", "http", "download":
	default:
		fail(w, http.StatusBadRequest, "type must be icmp, tcp, http or download")
		return
	}
	if t.Type == "download" && t.IntervalSeconds < 600 {
		t.IntervalSeconds = 600
	}
	if t.IntervalSeconds < 5 {
		t.IntervalSeconds = 30
	}
	t.ID = idOf(r)
	if err := h.Store.SavePingTask(r.Context(), &t); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, t)
}

func (h *handlers) deletePingTask(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeletePingTask(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getNodeProbe(w http.ResponseWriter, r *http.Request) {
	np, err := h.Store.NodeProbe(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	out := map[string]any{"probe": np, "billed": np.Billed()}
	if h.Probe != nil {
		if l, ok := h.Probe.Live(np.NodeID); ok {
			out["live"] = l
		}
	}
	ok(w, out)
}

func (h *handlers) putNodeProbe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hidden     bool
		Info       store.NodeProbeInfo
		LimitBytes int64
		ResetDay   int
		Mode       string
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if err := h.Store.UpdateNodeProbe(r.Context(), idOf(r), in.Hidden, in.Info, in.LimitBytes, in.ResetDay, in.Mode); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	h.getNodeProbe(w, r)
}

func (h *handlers) resetNodeTraffic(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.ResetNodeTraffic(r.Context(), idOf(r), time.Now()); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic80")
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic100")
	h.getNodeProbe(w, r)
}

// ---- subscription adjustments / renewal view ----------------------------------------------

func (h *handlers) adjustSubscription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AddDays       int
		QuotaOverride *int64
		ResetDay      *int
		ResetUsage    bool
		SubID         int64
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	sub, err := h.Store.AdjustSubscription(r.Context(), idOf(r), store.SubAdjust{SubID: in.SubID, AddDays: in.AddDays, QuotaOverride: in.QuotaOverride, ResetDay: in.ResetDay, ResetUsage: in.ResetUsage}, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNoActiveSubscription) {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, sub)
}

// cancelQueuedSub removes a queued (not yet started) plan from a user.
func (h *handlers) cancelQueuedSub(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err := h.Store.CancelQueued(r.Context(), idOf(r), sid); err != nil {
		fail(w, http.StatusNotFound, "no such queued subscription")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) renewals(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ExpiringUsers(r.Context(), queryInt(r, "limit", 200))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

// ---- personal API tokens -------------------------------------------------------------

func (h *handlers) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListAPITokens(r.Context(), userFrom(r).ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

func (h *handlers) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !decode(r, &in) || strings.TrimSpace(in.Name) == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	plain, tok, err := h.Store.CreateAPIToken(r.Context(), userFrom(r).ID, strings.TrimSpace(in.Name))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"token": plain, "id": tok.ID, "name": tok.Name})
}

func (h *handlers) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteAPIToken(r.Context(), userFrom(r).ID, idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- two-factor for staff -------------------------------------------------------------

// totpSetup stores a new (not yet enforced) secret and returns the otpauth URI.
func (h *handlers) totpSetup(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	secret := auth.NewTOTPSecret()
	if err := h.Store.SetTOTP(r.Context(), u.ID, secret, false); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"secret": secret, "uri": auth.TOTPURI(firstNonEmpty(h.SiteName, "Captain"), u.Email, secret)})
}

func (h *handlers) totpEnable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	u := userFrom(r)
	secret, _, err := h.Store.TOTP(r.Context(), u.ID)
	if err != nil || secret == "" {
		fail(w, http.StatusBadRequest, "run setup first")
		return
	}
	if !auth.VerifyTOTP(secret, in.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "code does not match; check the app's clock")
		return
	}
	if err := h.Store.SetTOTP(r.Context(), u.ID, secret, true); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"totp": true})
}

func (h *handlers) totpDisable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	u := userFrom(r)
	secret, enabled, _ := h.Store.TOTP(r.Context(), u.ID)
	if enabled && !auth.VerifyTOTP(secret, in.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "current authenticator code required")
		return
	}
	if err := h.Store.SetTOTP(r.Context(), u.ID, "", false); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"totp": false})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- temporary subscription links ----------------------------------------------------

// userEntries lists the servers one user gets, each with that user's share
// link and the blacklist flag.
func (h *handlers) userEntries(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	u, err := h.Store.UserByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	svc := &service.Subscription{Store: h.Store}
	list, err := svc.EntryLinks(r.Context(), u)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

// putUserEntries replaces the user's blacklist: {"blocked": [entry ids]}.
func (h *handlers) putUserEntries(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if _, err := h.Store.UserByID(r.Context(), id); !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	var in struct {
		Blocked []int64 `json:"blocked"`
	}
	if !decode(r, &in) {
		return
	}
	if err := h.Store.SetUserEntryBlocks(r.Context(), id, in.Blocked); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.userEntries(w, r)
}

func (h *handlers) listSubLinks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListSubLinks(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	base := ""
	if h.SubLinks != nil {
		base = strings.TrimSuffix(h.SubLinks.URL(r.Context(), "x"), "/sub/x")
		base = strings.TrimSuffix(base, "/s/x")
		if i := strings.Index(base, "/s/"); i > 0 {
			base = base[:i]
		}
	}
	out := make([]map[string]any, 0, len(list))
	for _, l := range list {
		out = append(out, map[string]any{"id": l.ID, "code": l.Code, "kind": l.Kind, "max_uses": l.MaxUses, "uses": l.Uses, "expires_at": l.ExpiresAt, "enabled": l.Enabled, "created_at": l.CreatedAt, "url": base + "/s/" + l.Code})
	}
	ok(w, out)
}

func (h *handlers) createTempLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MaxUses int
		Hours   int
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if in.MaxUses <= 0 && in.Hours <= 0 {
		fail(w, http.StatusBadRequest, "set a use limit and/or an expiry")
		return
	}
	if _, err := h.Store.UserByID(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	l, err := h.Store.CreateTempLink(r.Context(), idOf(r), in.MaxUses, time.Duration(in.Hours)*time.Hour)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, l)
}

func (h *handlers) deleteSubLink(w http.ResponseWriter, r *http.Request) {
	lid, _ := strconv.ParseInt(r.PathValue("lid"), 10, 64)
	if err := h.Store.DeleteSubLink(r.Context(), idOf(r), lid); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- komari -------------------------------------------------------------------------

func (h *handlers) getKomari(w http.ResponseWriter, r *http.Request) {
	var v store.KomariSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingKomari, &v)
	ok(w, map[string]any{"enabled": v.Enabled, "server": v.Server, "interval": v.Interval, "has_key": v.Key != ""})
}

// putKomari stores the setting; a blank key keeps the stored one, "-" clears it.
func (h *handlers) putKomari(w http.ResponseWriter, r *http.Request) {
	var in store.KomariSettings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur store.KomariSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingKomari, &cur)
	in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
	if in.Enabled && !strings.HasPrefix(in.Server, "http://") && !strings.HasPrefix(in.Server, "https://") {
		fail(w, http.StatusBadRequest, "Komari URL must start with http:// or https://")
		return
	}
	if in.Interval < 0 || in.Interval > 300 {
		fail(w, http.StatusBadRequest, "interval must be 0-300 seconds")
		return
	}
	switch strings.TrimSpace(in.Key) {
	case "":
		in.Key = cur.Key
	case "-":
		in.Key = ""
	default:
		in.Key = strings.TrimSpace(in.Key)
	}
	if in.Enabled && in.Key == "" {
		fail(w, http.StatusBadRequest, "the auto-discovery key is required to register nodes")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingKomari, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"enabled": in.Enabled, "server": in.Server, "interval": in.Interval, "has_key": in.Key != ""})
}
