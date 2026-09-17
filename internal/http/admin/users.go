package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type userView struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	UUID         string     `json:"uuid"`
	SubToken     string     `json:"sub_token"`
	SubURL       string     `json:"sub_url"`
	GroupID      *int64     `json:"group_id"`
	BalanceCents int64      `json:"balance_cents"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	PlanName     string     `json:"plan_name"`
	ExpiresAt    *time.Time `json:"expires_at"`
	QuotaBytes   int64      `json:"quota_bytes"`
	UsedBytes    int64      `json:"used_bytes"`
	SubUsable    bool       `json:"sub_usable"`
	SubCount     int        `json:"sub_count"`
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
		serverErr(w, err)
		return
	}
	out := make([]userView, 0, len(rows))
	for _, row := range rows {
		u := row.User
		out = append(out, userView{ID: u.ID, Email: u.Email, UUID: u.UUID, SubToken: u.SubToken, SubURL: h.subURL(r.Context(), u.SubToken), GroupID: u.GroupID, BalanceCents: u.BalanceCents, Status: u.Status, CreatedAt: u.CreatedAt,
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
		serverErr(w, err)
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
		serverErr(w, err)
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
	if nodes, err := h.Store.ListNodes(r.Context()); err == nil {
		relay := map[string]bool{}
		for _, n := range nodes {
			for _, a := range []string{n.PublicAddr, n.InternalAddr, n.V6Addr} {
				if a != "" {
					relay[a] = true
				}
			}
		}
		for i := range devices {
			devices[i].ViaRelay = relay[devices[i].IP]
		}
	}
	all, _ := h.Store.Subscriptions(r.Context(), id)
	names := map[int64]string{}
	subs := make([]map[string]any, 0, len(all))
	for _, s := range all {
		name, seen := names[s.PlanID]
		if !seen {
			if p, err := h.Store.PlanByID(r.Context(), s.PlanID); err == nil {
				name = p.Name
			}
			names[s.PlanID] = name
		}
		subs = append(subs, map[string]any{"id": s.ID, "plan_id": s.PlanID, "plan_name": name, "status": s.Status, "starts_at": s.StartsAt, "expires_at": s.ExpiresAt, "reset_at": s.ResetAt,
			"quota_bytes": s.QuotaBytes, "used_bytes": s.UsedUpBytes + s.UsedDownBytes, "usable": s.Status == "active" && s.Usable(time.Now()), "period_days": s.PeriodDays})
	}
	hwids, _ := h.Store.HwidDevices(r.Context(), id)
	if hwids == nil {
		hwids = []store.HwidDevice{}
	}
	dyn, _ := h.Store.DynLimitFor(r.Context(), id, time.Now())
	reqs, _ := h.Store.SubRequests(r.Context(), id, 50)
	if reqs == nil {
		reqs = []store.SubRequest{}
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken, "sub_url": h.subURL(r.Context(), u.SubToken), "group_id": u.GroupID, "status": u.Status,
		"invite_code": u.InviteCode, "invited_by": u.InvitedBy, "hwid_limit": u.HwidLimit, "first_connected_at": u.FirstConnectedAt,
		"balance_cents": u.BalanceCents, "created_at": u.CreatedAt, "subscription": sub, "subscriptions": subs, "orders": orders, "devices": devices,
		"hwid_devices": hwids, "sub_requests": reqs, "dyn_limit": dyn})
}

// deleteHwidDevice forgets one HWID device so the user can register a
// new one (the "kick" in the user drawer).
func (h *handlers) deleteHwidDevice(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteHwidDevice(r.Context(), id, r.PathValue("hwid")); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// putHwidLimit sets the per-user HWID device limit: null follows the plan,
// 0 is unlimited.
func (h *handlers) putHwidLimit(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct{ Limit *int }
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Limit != nil && *in.Limit < 0 {
		fail(w, http.StatusBadRequest, "Limit must be >= 0")
		return
	}
	if err := h.Store.SetUserHwidLimit(r.Context(), id, in.Limit); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct {
		Status   string
		GroupID  *int64
		Password string
	}
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if !readJSON(w, r, &in) {
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
			serverErr(w, err)
			return
		}
	}
	if err := h.Store.UpdateUser(r.Context(), id, in.Status, in.GroupID, hash); err != nil {
		serverErr(w, err)
		return
	}
	if hash != "" || in.Status != "active" {
		_ = h.Store.DeleteUserSessions(r.Context(), id)
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
		serverErr(w, err)
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
		serverErr(w, err)
		return
	}
	_ = h.Store.RotateShortCode(r.Context(), id)
	ok(w, map[string]string{"sub_token": tok, "sub_url": h.subURL(r.Context(), tok)})
}

func (h *handlers) adjustBalance(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct{ DeltaCents int64 }
	if !okID || !decode(r, &in) || in.DeltaCents == 0 {
		fail(w, http.StatusBadRequest, "DeltaCents is required")
		return
	}
	if err := h.Store.AdjustBalance(r.Context(), id, in.DeltaCents); err != nil {
		serverErr(w, err)
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
	var in struct {
		PlanID     int64
		Activation string // "" stack (or replace under single-plan), "queue", "replace"
	}
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if !readJSON(w, r, &in) {
		return
	}
	plan, err := h.Store.PlanByID(r.Context(), in.PlanID)
	if err != nil {
		fail(w, http.StatusNotFound, "plan not found")
		return
	}
	mode := h.Store.DefaultGrantMode(r.Context())
	switch in.Activation {
	case "queue":
		mode = store.GrantQueue
	case "replace":
		mode = store.GrantReplace
	}
	sub, err := h.Store.GrantSubscriptionMode(r.Context(), userID, plan, time.Now(), mode)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, sub)
}

func (h *handlers) listOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const per = 50
	rows, total, err := h.Store.ListOrders(r.Context(), q.Get("status"), per, (page-1)*per)
	if err != nil {
		serverErr(w, err)
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
