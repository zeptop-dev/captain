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

	"gitlab.com/boyang-hu/captain/internal/auth"
	"gitlab.com/boyang-hu/captain/internal/domain"
	"gitlab.com/boyang-hu/captain/internal/store"
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
}

const cookieName = "captain_session"

type handlers struct{ Deps }

// Register mounts the admin routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/admin/login", h.login)
	mux.HandleFunc("POST /api/admin/logout", h.logout)
	mux.HandleFunc("GET /api/admin/me", h.requireAdmin(h.me))
	mux.HandleFunc("GET /api/admin/nodes", h.requireAdmin(h.listNodes))
	mux.HandleFunc("POST /api/admin/nodes", h.requireAdmin(h.createNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/inbounds", h.requireAdmin(h.createInbound))
	mux.HandleFunc("POST /api/admin/users", h.requireAdmin(h.createUser))
	mux.HandleFunc("POST /api/admin/plans", h.requireAdmin(h.createPlan))
	mux.HandleFunc("POST /api/admin/groups", h.requireAdmin(h.createGroup))
	mux.HandleFunc("POST /api/admin/users/{id}/grant", h.requireAdmin(h.grantPlan))
}

type ctxKey struct{}

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

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

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role})
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.Store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, nodes)
}

func (h *handlers) createNode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, PublicAddr, InternalAddr, V6Addr, MonitorURL string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, MonitorURL: in.MonitorURL}
	if err := h.Store.CreateNode(r.Context(), n, auth.PairCode(), 24*time.Hour); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, n) // includes PairCode once
}

func (h *handlers) createInbound(w http.ResponseWriter, r *http.Request) {
	nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad node id")
		return
	}
	var ib domain.Inbound
	if err := json.NewDecoder(r.Body).Decode(&ib); err != nil || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.NodeID = nodeID
	ib.Enabled = true
	if err := h.Store.CreateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Email == "" || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "email and a password of 8+ chars are required")
		return
	}
	u, err := NewUser(in.Email, in.Password, "user")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken})
}

func (h *handlers) createPlan(w http.ResponseWriter, r *http.Request) {
	var p domain.Plan
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Name == "" {
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

func (h *handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
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

// grantPlan gives a user a plan directly (manual order).
func (h *handlers) grantPlan(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad user id")
		return
	}
	var in struct{ PlanID int64 }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
