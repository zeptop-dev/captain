// Package payment serves gateway callbacks under /api/payment.
package payment

import (
	"errors"
	"log/slog"
	"net/http"

	"gitlab.com/boyang-hu/captain/internal/payment"
	"gitlab.com/boyang-hu/captain/internal/payment/epay"
	"gitlab.com/boyang-hu/captain/internal/service"
	"gitlab.com/boyang-hu/captain/internal/store"
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
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if gw.Name() == "epay" && n.Paid {
				order, err := d.Store.OrderByNo(r.Context(), n.OrderNo)
				if err != nil || !epay.VerifyAmount(r, order.AmountCents) {
					d.Log.Warn("epay amount mismatch", "order", n.OrderNo)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
			}
			if _, err := d.Orders.Settle(r.Context(), n); err != nil && !errors.Is(err, store.ErrNotFound) {
				d.Log.Error("settle failed", "order", n.OrderNo, "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(n.Response))
		}
		mux.HandleFunc("GET /api/payment/"+name+"/notify", handle)
		mux.HandleFunc("POST /api/payment/"+name+"/notify", handle)
	}
	// EPay's return_url lands the browser here (same params as notify);
	// settle if possible, then send the user to the portal.
	mux.HandleFunc("GET /api/payment/epay/return", func(w http.ResponseWriter, r *http.Request) {
		if gw, ok := d.Gateways["epay"]; ok {
			if n, err := gw.Notify(r); err == nil {
				_, _ = d.Orders.Settle(r.Context(), n)
			}
		}
		http.Redirect(w, r, d.ReturnTo, http.StatusFound)
	})
}
