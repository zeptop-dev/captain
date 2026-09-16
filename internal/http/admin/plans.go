package admin

import (
	"net/http"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

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

func (h *handlers) listCoupons(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListCoupons(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

func (h *handlers) createCoupon(w http.ResponseWriter, r *http.Request) {
	var c domain.Coupon
	if !decode(r, &c) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if strings.TrimSpace(c.Code) == "" {
		c.Code = store.GenerateCouponCode()
	}
	if err := h.Store.CreateCoupon(r.Context(), &c); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, c)
}

func (h *handlers) updateCoupon(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var c domain.Coupon
	if !okID || !decode(r, &c) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	c.ID = id
	if err := h.Store.UpdateCoupon(r.Context(), &c); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, c)
}

func (h *handlers) deleteCoupon(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteCoupon(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}
