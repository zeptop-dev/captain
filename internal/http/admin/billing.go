package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getTrial(w http.ResponseWriter, r *http.Request) {
	getSetting[store.TrialSettings](h, w, r, store.SettingTrial, nil)
}

func (h *handlers) putTrial(w http.ResponseWriter, r *http.Request) {
	putSetting(h, w, r, store.SettingTrial, func(ctx context.Context, v *store.TrialSettings) string {
		if v.PlanID != 0 {
			if _, err := h.Store.PlanByID(ctx, v.PlanID); errors.Is(err, store.ErrNotFound) {
				return "unknown plan"
			}
		}
		return ""
	})
}

func (h *handlers) getSurplus(w http.ResponseWriter, r *http.Request) {
	getSetting[store.SurplusSettings](h, w, r, store.SettingSurplus, nil)
}

func (h *handlers) putSurplus(w http.ResponseWriter, r *http.Request) {
	putSetting[store.SurplusSettings](h, w, r, store.SettingSurplus, nil)
}

func (h *handlers) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListWithdrawals(r.Context(), 0, r.URL.Query().Get("status"), queryInt(r, "limit", 200))
	if err != nil {
		serverErr(w, err)
		return
	}
	if list == nil {
		list = []store.Withdrawal{}
	}
	ok(w, list)
}

func (h *handlers) withdrawalStatus(w http.ResponseWriter, r *http.Request) {
	var in struct{ Status, Note string }
	if !readJSON(w, r, &in) {
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

func (h *handlers) adjustSubscription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AddDays       int
		QuotaOverride *int64
		ResetDay      *int
		ResetUsage    bool
		SubID         int64
	}
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
		return
	}
	ok(w, list)
}
