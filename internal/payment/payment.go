// Package payment defines the gateway interface and the order-side hooks.
package payment

import (
	"context"
	"net/http"

	"gitlab.com/boyang-hu/captain/internal/domain"
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
	// Response is what the gateway expects back on success ("success", or JSON).
	Response string
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
