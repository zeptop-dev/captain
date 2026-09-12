// Package admin serves the admin console API under /api/admin.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// SessionStore is the cookie session backend.
type SessionStore interface {
	Create(ctx context.Context, userID int64) (*domain.Session, error)
	Resolve(ctx context.Context, id string) (*domain.User, error)
	Delete(ctx context.Context, id string) error
}

// Deps are the handlers' dependencies.
type Deps struct {
	Store    *store.Store
	Log      *slog.Logger
	Sessions SessionStore
	Version  string
}

const cookieName = "captain_session"

type handlers struct{ Deps }

// Register mounts the admin routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/admin/login", h.login)
	mux.HandleFunc("POST /api/admin/logout", h.logout)
	mux.HandleFunc("GET /api/admin/me", h.requireAdmin(h.me))
	mux.HandleFunc("GET /api/admin/dashboard", h.requireAdmin(h.dashboard))

	mux.HandleFunc("GET /api/admin/nodes", h.requireAdmin(h.listNodes))
	mux.HandleFunc("POST /api/admin/nodes", h.requireAdmin(h.createNode))
	mux.HandleFunc("GET /api/admin/nodes/{id}", h.requireAdmin(h.getNode))
	mux.HandleFunc("PATCH /api/admin/nodes/{id}", h.requireAdmin(h.updateNode))
	mux.HandleFunc("DELETE /api/admin/nodes/{id}", h.requireAdmin(h.deleteNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/repair", h.requireAdmin(h.repairNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/inbounds", h.requireAdmin(h.createInbound))
	mux.HandleFunc("PATCH /api/admin/inbounds/{id}", h.requireAdmin(h.updateInbound))
	mux.HandleFunc("DELETE /api/admin/inbounds/{id}", h.requireAdmin(h.deleteInbound))

	mux.HandleFunc("GET /api/admin/users", h.requireAdmin(h.listUsers))
	mux.HandleFunc("POST /api/admin/users", h.requireAdmin(h.createUser))
	mux.HandleFunc("GET /api/admin/users/{id}", h.requireAdmin(h.getUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", h.requireAdmin(h.updateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", h.requireAdmin(h.deleteUser))
	mux.HandleFunc("POST /api/admin/users/{id}/grant", h.requireAdmin(h.grantPlan))
	mux.HandleFunc("POST /api/admin/users/{id}/balance", h.requireAdmin(h.adjustBalance))
	mux.HandleFunc("POST /api/admin/users/{id}/rotate-token", h.requireAdmin(h.rotateToken))

	mux.HandleFunc("GET /api/admin/plans", h.requireAdmin(h.listPlans))
	mux.HandleFunc("POST /api/admin/plans", h.requireAdmin(h.createPlan))
	mux.HandleFunc("PATCH /api/admin/plans/{id}", h.requireAdmin(h.updatePlan))
	mux.HandleFunc("DELETE /api/admin/plans/{id}", h.requireAdmin(h.deletePlan))

	mux.HandleFunc("GET /api/admin/groups", h.requireAdmin(h.listGroups))
	mux.HandleFunc("POST /api/admin/groups", h.requireAdmin(h.createGroup))

	mux.HandleFunc("GET /api/admin/entries", h.requireAdmin(h.listEntries))
	mux.HandleFunc("POST /api/admin/entries", h.requireAdmin(h.createEntry))
	mux.HandleFunc("PATCH /api/admin/entries/{id}", h.requireAdmin(h.updateEntry))
	mux.HandleFunc("DELETE /api/admin/entries/{id}", h.requireAdmin(h.deleteEntry))

	mux.HandleFunc("GET /api/admin/orders", h.requireAdmin(h.listOrders))
}

type ctxKey struct{}

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func decode(r *http.Request, v any) bool { return json.NewDecoder(r.Body).Decode(v) == nil }

func (h *handlers) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			fail(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, err := h.Sessions.Resolve(r.Context(), c.Value)
		if err != nil {
			h.Log.Error("session", "err", err)
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		if u == nil || !u.IsAdmin() || u.Status != "active" {
			fail(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// --- auth ---

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	u, err := h.Store.UserByEmail(r.Context(), in.Email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !auth.VerifyPassword(u.PasswordHash, in.Password)) {
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !u.IsAdmin() {
		fail(w, http.StatusForbidden, "admin only")
		return
	}
	sess, err := h.Sessions.Create(r.Context(), u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt})
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role})
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = h.Sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role, "version": h.Version})
}

func (h *handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	stats, err := h.Store.Dashboard(r.Context(), now)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	series, err := h.Store.TrafficSeries(r.Context(), now, 14)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"stats": stats, "traffic": series})
}

// --- nodes ---

type nodeView struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	PublicAddr   string     `json:"public_addr"`
	InternalAddr string     `json:"internal_addr"`
	V6Addr       string     `json:"v6_addr"`
	MonitorURL   string     `json:"monitor_url"`
	Version      string     `json:"version"`
	Platform     string     `json:"platform"`
	Hostname     string     `json:"hostname"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	Online       bool       `json:"online"`
	Paired       bool       `json:"paired"`
	PairCode     string     `json:"pair_code,omitempty"`
	TrafficToday int64      `json:"traffic_today_bytes"`
	Inbounds     int        `json:"inbounds"`
}

func toNodeView(n *domain.Node, at time.Time) nodeView {
	return nodeView{
		ID: n.ID, Name: n.Name, PublicAddr: n.PublicAddr, InternalAddr: n.InternalAddr, V6Addr: n.V6Addr, MonitorURL: n.MonitorURL,
		Version: n.Version, Platform: n.Platform, Hostname: n.Hostname, LastSeenAt: n.LastSeenAt,
		Online: n.LastSeenAt != nil && at.Sub(*n.LastSeenAt) < 3*time.Minute, Paired: n.Paired, PairCode: n.PairCode,
	}
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	nodes, err := h.Store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	traffic, _ := h.Store.NodeTrafficToday(r.Context(), now)
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		v := toNodeView(n, now)
		v.PairCode = "" // only shown on create/repair
		v.TrafficToday = traffic[n.ID]
		if ibs, err := h.Store.AllInboundsByNode(r.Context(), n.ID); err == nil {
			v.Inbounds = len(ibs)
		}
		out = append(out, v)
	}
	ok(w, out)
}

type nodeInput struct {
	Name, PublicAddr, InternalAddr, V6Addr, MonitorURL string
}

func (h *handlers) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, MonitorURL: in.MonitorURL}
	if err := h.Store.CreateNode(r.Context(), n, auth.PairCode(), 24*time.Hour); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, toNodeView(n, time.Now())) // includes the pairing code once
}

func (h *handlers) getNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	v := toNodeView(n, time.Now())
	if n.Paired {
		v.PairCode = ""
	}
	inbounds, _ := h.Store.AllInboundsByNode(r.Context(), id)
	if inbounds == nil {
		inbounds = []*domain.Inbound{}
	}
	status, _ := h.Store.NodeStatus(r.Context(), id)
	ok(w, map[string]any{"node": v, "inbounds": inbounds, "status": status})
}

func (h *handlers) updateNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in nodeInput
	if !okID || !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{ID: id, Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, MonitorURL: in.MonitorURL}
	if err := h.Store.UpdateNode(r.Context(), n); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteNode(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// repairNode issues a new pairing code and revokes the old token.
func (h *handlers) repairNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	code := auth.PairCode()
	if err := h.Store.ResetPairCode(r.Context(), id, code, 24*time.Hour); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]string{"pair_code": code})
}

func (h *handlers) createInbound(w http.ResponseWriter, r *http.Request) {
	nodeID, okID := pathID(r)
	var ib domain.Inbound
	if !okID || !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.NodeID = nodeID
	ib.Enabled = true
	if err := h.Store.CreateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) updateInbound(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	cur, err := h.Store.InboundByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "inbound not found")
		return
	}
	ib := *cur
	if !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.ID, ib.NodeID = cur.ID, cur.NodeID
	if err := h.Store.UpdateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteInbound(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// --- users ---

type userView struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	UUID         string     `json:"uuid"`
	SubToken     string     `json:"sub_token"`
	GroupID      *int64     `json:"group_id"`
	BalanceCents int64      `json:"balance_cents"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	PlanName     string     `json:"plan_name"`
	ExpiresAt    *time.Time `json:"expires_at"`
	QuotaBytes   int64      `json:"quota_bytes"`
	UsedBytes    int64      `json:"used_bytes"`
	SubUsable    bool       `json:"sub_usable"`
}

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const per = 50
	rows, total, err := h.Store.ListUsers(r.Context(), q.Get("q"), per, (page-1)*per, time.Now())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]userView, 0, len(rows))
	for _, row := range rows {
		u := row.User
		out = append(out, userView{ID: u.ID, Email: u.Email, UUID: u.UUID, SubToken: u.SubToken, GroupID: u.GroupID, BalanceCents: u.BalanceCents, Status: u.Status, CreatedAt: u.CreatedAt,
			PlanName: row.PlanName, ExpiresAt: row.ExpiresAt, QuotaBytes: row.QuotaBytes, UsedBytes: row.UsedBytes, SubUsable: row.SubUsable})
	}
	ok(w, map[string]any{"items": out, "total": total, "page": page, "per_page": per})
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(r, &in) || in.Email == "" || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "email and a password of 8+ chars are required")
		return
	}
	u, err := NewUser(in.Email, in.Password, "user")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already exists")
		return
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken})
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	u, err := h.Store.UserByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	sub, err := h.Store.ActiveSubscription(r.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	orders, _ := h.Store.OrdersByUser(r.Context(), id, 20)
	if orders == nil {
		orders = []*domain.Order{}
	}
	devices, _ := h.Store.OnlineDevices(r.Context(), id, time.Now().Add(-5*time.Minute))
	if devices == nil {
		devices = []store.OnlineDevice{}
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken, "group_id": u.GroupID, "status": u.Status,
		"balance_cents": u.BalanceCents, "created_at": u.CreatedAt, "subscription": sub, "orders": orders, "devices": devices})
}

func (h *handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct {
		Status   string
		GroupID  *int64
		Password string
	}
	if !okID || !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if in.Status != "active" && in.Status != "banned" {
		fail(w, http.StatusBadRequest, "status must be active or banned")
		return
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < 8 {
			fail(w, http.StatusBadRequest, "password too short")
			return
		}
		var err error
		if hash, err = auth.HashPassword(in.Password); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := h.Store.UpdateUser(r.Context(), id, in.Status, in.GroupID, hash); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteUser(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) rotateToken(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	tok := auth.Token(24)
	if err := h.Store.RotateSubToken(r.Context(), id, tok); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]string{"sub_token": tok})
}

func (h *handlers) adjustBalance(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct{ DeltaCents int64 }
	if !okID || !decode(r, &in) || in.DeltaCents == 0 {
		fail(w, http.StatusBadRequest, "DeltaCents is required")
		return
	}
	if err := h.Store.AdjustBalance(r.Context(), id, in.DeltaCents); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := h.Store.UserByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	ok(w, map[string]any{"id": u.ID, "balance_cents": u.BalanceCents})
}

// grantPlan gives a user a plan directly (manual order).
func (h *handlers) grantPlan(w http.ResponseWriter, r *http.Request) {
	userID, okID := pathID(r)
	var in struct{ PlanID int64 }
	if !okID || !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	plan, err := h.Store.PlanByID(r.Context(), in.PlanID)
	if err != nil {
		fail(w, http.StatusNotFound, "plan not found")
		return
	}
	sub, err := h.Store.GrantSubscription(r.Context(), userID, plan, time.Now())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, sub)
}

// --- plans / groups ---

func (h *handlers) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Store.ListPlans(r.Context(), false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plans == nil {
		plans = []*domain.Plan{}
	}
	ok(w, plans)
}

func (h *handlers) createPlan(w http.ResponseWriter, r *http.Request) {
	var p domain.Plan
	if !decode(r, &p) || p.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	p.Enabled = true
	if err := h.Store.CreatePlan(r.Context(), &p); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, p)
}

func (h *handlers) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	cur, err := h.Store.PlanByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "plan not found")
		return
	}
	p := *cur
	if !decode(r, &p) || p.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	p.ID = cur.ID
	if err := h.Store.UpdatePlan(r.Context(), &p); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, p)
}

func (h *handlers) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeletePlan(r.Context(), id); err != nil {
		fail(w, http.StatusConflict, "plan is referenced by orders or subscriptions; disable it instead")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.Store.ListGroups(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []domain.Group{}
	}
	ok(w, groups)
}

func (h *handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	id, err := h.Store.CreateGroup(r.Context(), in.Name)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, map[string]any{"id": id, "name": in.Name})
}

// --- entries ---

func (h *handlers) listEntries(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListEntries(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*domain.Entry{}
	}
	ok(w, list)
}

func (h *handlers) createEntry(w http.ResponseWriter, r *http.Request) {
	var e domain.Entry
	if !decode(r, &e) || e.Name == "" || e.InboundID == 0 || e.DisplayHost == "" || e.DisplayPort == 0 {
		fail(w, http.StatusBadRequest, "name, inbound_id, display_host and display_port are required")
		return
	}
	e.Enabled = true
	if err := h.Store.CreateEntry(r.Context(), &e); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, e)
}

func (h *handlers) updateEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var e domain.Entry
	if !okID || !decode(r, &e) || e.Name == "" || e.InboundID == 0 || e.DisplayHost == "" || e.DisplayPort == 0 {
		fail(w, http.StatusBadRequest, "name, inbound_id, display_host and display_port are required")
		return
	}
	e.ID = id
	if err := h.Store.UpdateEntry(r.Context(), &e); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, e)
}

func (h *handlers) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteEntry(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// --- orders ---

func (h *handlers) listOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const per = 50
	rows, total, err := h.Store.ListOrders(r.Context(), q.Get("status"), per, (page-1)*per)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	type row struct {
		domain.Order
		Email    string `json:"email"`
		PlanName string `json:"plan_name"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{Order: r.Order, Email: r.Email, PlanName: r.PlanName})
	}
	ok(w, map[string]any{"items": out, "total": total, "page": page, "per_page": per})
}

// NewUser builds a user with fresh identity secrets.
func NewUser(email, password, role string) (*domain.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	return &domain.User{Email: email, PasswordHash: hash, Role: role, UUID: auth.UUID(), SubToken: auth.Token(24), Status: "active"}, nil
}

func ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
