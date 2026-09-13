package portal

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerOps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/portal/tickets", h.requireUser(h.tickets))
	mux.HandleFunc("POST /api/portal/tickets", h.requireUser(h.createTicket))
	mux.HandleFunc("GET /api/portal/tickets/{id}", h.requireUser(h.ticket))
	mux.HandleFunc("POST /api/portal/tickets/{id}/reply", h.requireUser(h.replyTicket))
	mux.HandleFunc("POST /api/portal/tickets/{id}/close", h.requireUser(h.closeTicket))
	mux.HandleFunc("POST /api/portal/redeem", h.requireUser(h.redeem))
	mux.HandleFunc("GET /api/portal/articles", h.requireUser(h.articles))
	mux.HandleFunc("GET /api/portal/articles/{id}", h.requireUser(h.article))
	mux.HandleFunc("GET /api/portal/clients", h.clients)
	mux.HandleFunc("GET /api/portal/telegram", h.requireUser(h.telegram))
	mux.HandleFunc("POST /api/portal/telegram/unbind", h.requireUser(h.telegramUnbind))
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// ---- tickets ----------------------------------------------------------------

func ticketView(t *domain.Ticket, msgs []domain.TicketMessage) map[string]any {
	v := map[string]any{"id": t.ID, "subject": t.Subject, "status": t.Status, "priority": t.Priority, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt}
	if msgs != nil {
		out := make([]map[string]any, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, map[string]any{"id": m.ID, "from_admin": m.FromAdmin, "body": m.Body, "created_at": m.CreatedAt})
		}
		v["messages"] = out
	}
	return v
}

func (h *handlers) tickets(w http.ResponseWriter, r *http.Request) {
	rows, _, err := h.Store.ListTickets(r.Context(), userFrom(r).ID, "", 100, 0)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		v := ticketView(&rows[i].Ticket, nil)
		v["messages"] = rows[i].Messages
		out = append(out, v)
	}
	ok(w, out)
}

func (h *handlers) createTicket(w http.ResponseWriter, r *http.Request) {
	var in struct{ Subject, Priority, Body string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Body) == "" {
		fail(w, http.StatusBadRequest, "subject and message are required")
		return
	}
	switch in.Priority {
	case "low", "high":
	default:
		in.Priority = "normal"
	}
	if len(in.Subject) > 200 || len(in.Body) > 20000 {
		fail(w, http.StatusBadRequest, "too long")
		return
	}
	u := userFrom(r)
	if _, open, _ := h.Store.ListTickets(r.Context(), u.ID, store.TicketOpen, 1, 0); open >= 5 {
		fail(w, http.StatusTooManyRequests, "you already have several open tickets; wait for a reply")
		return
	}
	t, err := h.Store.CreateTicket(r.Context(), u.ID, strings.TrimSpace(in.Subject), in.Priority, strings.TrimSpace(in.Body))
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.notifyAdminTicket(r, "🎫 New ticket #"+strconv.FormatInt(t.ID, 10)+" from "+u.Email+"\n"+t.Subject)
	msgs, _ := h.Store.TicketMessages(r.Context(), t.ID)
	ok(w, ticketView(t, msgs))
}

func (h *handlers) ownTicket(w http.ResponseWriter, r *http.Request) *domain.Ticket {
	t, err := h.Store.TicketByID(r.Context(), pathID(r))
	if err != nil || t.UserID != userFrom(r).ID {
		fail(w, http.StatusNotFound, "ticket not found")
		return nil
	}
	return t
}

func (h *handlers) ticket(w http.ResponseWriter, r *http.Request) {
	t := h.ownTicket(w, r)
	if t == nil {
		return
	}
	msgs, _ := h.Store.TicketMessages(r.Context(), t.ID)
	ok(w, ticketView(t, msgs))
}

func (h *handlers) replyTicket(w http.ResponseWriter, r *http.Request) {
	t := h.ownTicket(w, r)
	if t == nil {
		return
	}
	var in struct{ Body string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Body) == "" || len(in.Body) > 20000 {
		fail(w, http.StatusBadRequest, "message is required")
		return
	}
	if t.Status == store.TicketClosed {
		fail(w, http.StatusConflict, "ticket is closed")
		return
	}
	if err := h.Store.ReplyTicket(r.Context(), t.ID, false, strings.TrimSpace(in.Body)); err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.notifyAdminTicket(r, "🎫 Reply on ticket #"+strconv.FormatInt(t.ID, 10)+" from "+userFrom(r).Email+"\n"+t.Subject)
	h.ticket(w, r)
}

func (h *handlers) closeTicket(w http.ResponseWriter, r *http.Request) {
	t := h.ownTicket(w, r)
	if t == nil {
		return
	}
	_ = h.Store.SetTicketStatus(r.Context(), t.ID, store.TicketClosed)
	h.ticket(w, r)
}

// ---- gift codes ---------------------------------------------------------------

func (h *handlers) redeem(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Code) == "" {
		fail(w, http.StatusBadRequest, "code is required")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	g, err := h.Store.RedeemGiftCode(r.Context(), userFrom(r).ID, code, time.Now())
	switch {
	case errors.Is(err, store.ErrGiftInvalid), errors.Is(err, store.ErrNoSubscription):
		fail(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	ok(w, map[string]any{"kind": g.Kind, "value": g.Value, "plan_id": g.PlanID, "period_days": g.PeriodDays})
}

// ---- knowledge base -----------------------------------------------------------

func (h *handlers) articles(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListArticles(r.Context(), true)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{"id": a.ID, "title": a.Title, "category": a.Category, "lang": a.Lang, "updated_at": a.UpdatedAt})
	}
	ok(w, out)
}

func (h *handlers) article(w http.ResponseWriter, r *http.Request) {
	a, err := h.Store.ArticleByID(r.Context(), pathID(r))
	if err != nil || !a.Published {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	u := userFrom(r)
	body := strings.NewReplacer("{{sub_url}}", h.subURL(r.Context(), u.SubToken), "{{email}}", u.Email, "{{site_name}}", h.SiteName).Replace(a.Body)
	ok(w, map[string]any{"id": a.ID, "title": a.Title, "category": a.Category, "lang": a.Lang, "body": body, "updated_at": a.UpdatedAt})
}

func (h *handlers) clients(w http.ResponseWriter, r *http.Request) {
	var cs store.ClientsSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingClients, &cs)
	if cs.Items == nil {
		cs.Items = []store.ClientItem{}
	}
	ok(w, cs.Items)
}

// ---- telegram -------------------------------------------------------------------

func (h *handlers) telegram(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	out := map[string]any{"enabled": false}
	if h.Bot == nil {
		ok(w, out)
		return
	}
	s := h.Bot.Settings(r.Context())
	if s.BotToken == "" {
		ok(w, out)
		return
	}
	chat, _ := h.Store.TelegramID(r.Context(), u.ID)
	out["enabled"], out["bot"], out["bound"] = true, s.BotUsername, chat != 0
	if chat == 0 {
		code, err := h.Store.NewTelegramBindCode(r.Context(), u.ID, 30*time.Minute)
		if err == nil {
			out["code"] = code
			if s.BotUsername != "" {
				out["link"] = "https://t.me/" + s.BotUsername + "?start=" + code
			}
		}
	}
	ok(w, out)
}

func (h *handlers) telegramUnbind(w http.ResponseWriter, r *http.Request) {
	_ = h.Store.UnbindTelegram(r.Context(), userFrom(r).ID)
	ok(w, map[string]bool{"bound": false})
}

// notifyAdminTicket tells the operator chat when the setting allows it.
func (h *handlers) notifyAdminTicket(r *http.Request, text string) {
	if h.Bot == nil || !h.Bot.Settings(r.Context()).NotifyTicket {
		return
	}
	h.Notify.Admin(r.Context(), text)
}
