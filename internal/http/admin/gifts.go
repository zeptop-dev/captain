package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

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
		serverErr(w, err)
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
		serverErr(w, err)
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
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"batch": in.Batch, "codes": codes})
}

func (h *handlers) deleteGiftBatch(w http.ResponseWriter, r *http.Request) {
	n, err := h.Store.DeleteUnredeemedGiftCodes(r.Context(), r.PathValue("batch"))
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"deleted": n})
}
