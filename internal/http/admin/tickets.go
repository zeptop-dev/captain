package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

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
		serverErr(w, err)
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
		serverErr(w, err)
		return
	}
	if u, err := h.Store.UserByID(r.Context(), t.UserID); err == nil {
		h.Notify.User(r.Context(), u.ID, u.Email, "Ticket #"+strconv.FormatInt(t.ID, 10)+": "+t.Subject, strings.TrimSpace(in.Body))
	}
	h.writeTicket(w, r, t.ID)
}

func (h *handlers) ticketStatus(w http.ResponseWriter, r *http.Request) {
	var in struct{ Status string }
	if !readJSON(w, r, &in) {
		return
	}
	switch in.Status {
	case store.TicketOpen, store.TicketReplied, store.TicketClosed:
	default:
		fail(w, http.StatusBadRequest, "status must be open, replied or closed")
		return
	}
	if err := h.Store.SetTicketStatus(r.Context(), idOf(r), in.Status); err != nil {
		serverErr(w, err)
		return
	}
	h.writeTicket(w, r, idOf(r))
}
