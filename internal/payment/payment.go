// Package payment defines the gateway interface and the order-side hooks.
package payment

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
)

// Checkout is what the client needs to pay: a URL to open.
type Checkout struct {
	URL string
}

// Notification is a parsed, signature-verified payment callback.
type Notification struct {
	OrderNo    string
	GatewayRef string
	Paid       bool
	// AmountCents is the paid amount when the callback states it (0 =
	// unknown); the HTTP layer refuses to settle when it differs from the order.
	AmountCents int64
	// Response is what the gateway expects back on success ("success", or JSON).
	Response string
}

// ParseMoney converts "12.34" to 1234 cents; malformed input yields 0.
func ParseMoney(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	whole, frac, _ := strings.Cut(s, ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0
	}
	frac = (frac + "00")[:2]
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0
	}
	return w*100 + f
}

// Pager is implemented by gateways that serve their own checkout page
// (e.g. a QR code); it is mounted at GET /api/payment/<name>/page.
type Pager interface {
	ServePage(w http.ResponseWriter, r *http.Request)
}

// Gateway creates checkouts and parses callbacks.
type Gateway interface {
	Name() string
	// Create starts a payment for order and returns where to send the user.
	Create(ctx context.Context, order *domain.Order, plan *domain.Plan, user *domain.User, clientIP string) (*Checkout, error)
	// Notify verifies and parses an asynchronous callback. Implementations
	// must not trust any field before the signature is verified.
	Notify(r *http.Request) (*Notification, error)
}
