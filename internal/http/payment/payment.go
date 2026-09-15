// Package payment serves gateway callbacks under /api/payment.
package payment

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/zeptop-dev/captain/internal/metrics"
	"github.com/zeptop-dev/captain/internal/payment"
	"github.com/zeptop-dev/captain/internal/payment/epay"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Deps are the handlers' dependencies.
type Deps struct {
	Log      *slog.Logger
	Orders   *service.Orders
	ReturnTo string // portal URL to send the browser to after paying
	Gateways map[string]payment.Gateway
	Store    *store.Store
}

// Register mounts callback routes for every configured gateway.
func Register(mux *http.ServeMux, d Deps) {
	for name, gw := range d.Gateways {
		gw := gw
		handle := func(w http.ResponseWriter, r *http.Request) {
			n, err := gw.Notify(r)
			if err != nil {
				d.Log.Warn("payment callback rejected", "gateway", gw.Name(), "err", err)
				metrics.PaymentCallbackErrors.Inc(map[string]string{"gateway": gw.Name()})
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			// A signed callback still must match the order amount: a
			// gateway that lets the payer pick the amount would otherwise
			// settle a full plan for a token payment.
			if n.Paid && (gw.Name() == "epay" || n.AmountCents > 0) {
				order, err := d.Store.OrderByNo(r.Context(), n.OrderNo)
				if err != nil || (gw.Name() == "epay" && !epay.VerifyAmount(r, order.AmountCents)) || (n.AmountCents > 0 && n.AmountCents != order.AmountCents) {
					d.Log.Warn("payment amount mismatch", "gateway", gw.Name(), "order", n.OrderNo, "amount", n.AmountCents)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
			}
			if _, err := d.Orders.Settle(r.Context(), n); err != nil && !errors.Is(err, store.ErrNotFound) {
				d.Log.Error("settle failed", "order", n.OrderNo, "err", err)
				metrics.PaymentSettleErrors.Inc(map[string]string{"gateway": gw.Name()})
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(n.Response))
		}
		mux.HandleFunc("GET /api/payment/"+name+"/notify", handle)
		mux.HandleFunc("POST /api/payment/"+name+"/notify", handle)
		// Gateways that render their own checkout page (Alipay QR).
		if p, ok := gw.(payment.Pager); ok {
			mux.HandleFunc("GET /api/payment/"+name+"/page", p.ServePage)
		}
	}
	// EPay's return_url lands the browser here (same params as notify);
	// settle if possible, then send the user to the portal.
	mux.HandleFunc("GET /api/payment/epay/return", func(w http.ResponseWriter, r *http.Request) {
		if gw, ok := d.Gateways["epay"]; ok {
			if n, err := gw.Notify(r); err == nil && n.Paid {
				// Same amount check as /notify: a signed but under-paid
				// callback replayed here must not settle the order.
				if order, err := d.Store.OrderByNo(r.Context(), n.OrderNo); err == nil && epay.VerifyAmount(r, order.AmountCents) {
					_, _ = d.Orders.Settle(r.Context(), n)
				} else {
					d.Log.Warn("payment amount mismatch on return", "gateway", "epay", "order", n.OrderNo)
				}
			}
		}
		http.Redirect(w, r, d.ReturnTo, http.StatusFound)
	})
}
