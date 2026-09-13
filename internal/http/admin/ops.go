package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
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
	mux.HandleFunc("GET /api/admin/settings/clients", h.requireAdmin(h.getClients))
	mux.HandleFunc("PUT /api/admin/settings/clients", h.requireAdmin(h.putClients))
	mux.HandleFunc("GET /api/admin/settings/telegram", h.requireAdmin(h.getTelegram))
	mux.HandleFunc("PUT /api/admin/settings/telegram", h.requireAdmin(h.putTelegram))
	mux.HandleFunc("POST /api/admin/settings/telegram/test", h.requireAdmin(h.testTelegram))
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
