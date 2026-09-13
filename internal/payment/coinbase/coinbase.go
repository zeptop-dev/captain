// Package coinbase implements Coinbase Commerce hosted charges: one JSON
// POST creates a charge, the user pays on Coinbase's page, and the webhook is
// verified with HMAC-SHA256 of the raw body (X-CC-Webhook-Signature).
package coinbase

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
)

// Config is the Commerce account setup.
type Config struct {
	APIKey        string
	WebhookSecret string // "shared secret" from the webhook settings page
	Currency      string // local price currency, e.g. CNY or USD; default CNY
	RedirectURL   string // after payment
	CancelURL     string
	APIBase       string // override for tests; default https://api.commerce.coinbase.com
}

// Gateway is a Coinbase Commerce client.
type Gateway struct {
	cfg  Config
	http *http.Client
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.APIKey == "" || cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("coinbase: api_key and webhook_secret are required")
	}
	if cfg.Currency == "" {
		cfg.Currency = "CNY"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.commerce.coinbase.com"
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *Gateway) Name() string { return "coinbase" }

func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// Create makes a fixed-price charge and returns its hosted URL.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, plan *domain.Plan, _ *domain.User, _ string) (*payment.Checkout, error) {
	body, _ := json.Marshal(map[string]any{
		"name":         plan.Name,
		"description":  "Order " + order.No,
		"pricing_type": "fixed_price",
		"local_price":  map[string]string{"amount": money(order.AmountCents), "currency": strings.ToUpper(g.cfg.Currency)},
		"metadata":     map[string]string{"order_no": order.No},
		"redirect_url": g.cfg.RedirectURL,
		"cancel_url":   g.cfg.CancelURL,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.APIBase+"/charges", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CC-Api-Key", g.cfg.APIKey)
	req.Header.Set("X-CC-Version", "2018-03-22")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coinbase: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			ID        string `json:"id"`
			HostedURL string `json:"hosted_url"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("coinbase: bad response: %w", err)
	}
	if resp.StatusCode/100 != 2 || out.Data.HostedURL == "" {
		return nil, fmt.Errorf("coinbase: create charge failed: %s", out.Error.Message)
	}
	return &payment.Checkout{URL: out.Data.HostedURL}, nil
}

// SignForTest computes the webhook signature of body.
func SignForTest(body []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Notify verifies the webhook and reports whether the charge is confirmed.
// charge:confirmed means funds arrived; charge:resolved covers a delayed or
// overpaid charge the merchant marked resolved.
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("coinbase: read body: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(r.Header.Get("X-CC-Webhook-Signature"))), []byte(SignForTest(body, g.cfg.WebhookSecret))) != 1 {
		return nil, fmt.Errorf("coinbase: bad signature")
	}
	var ev struct {
		Event struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Data struct {
				Code     string            `json:"code"`
				Metadata map[string]string `json:"metadata"`
			} `json:"data"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("coinbase: bad json")
	}
	orderNo := ev.Event.Data.Metadata["order_no"]
	if orderNo == "" {
		return nil, fmt.Errorf("coinbase: event without order_no")
	}
	return &payment.Notification{
		OrderNo:    orderNo,
		GatewayRef: ev.Event.Data.Code,
		Paid:       ev.Event.Type == "charge:confirmed" || ev.Event.Type == "charge:resolved",
		Response:   "ok",
	}, nil
}
