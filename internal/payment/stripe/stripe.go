// Package stripe implements Stripe Checkout with webhook verification, using
// the REST API directly (no SDK): one form-encoded POST to create a session,
// and HMAC-SHA256 verification of the Stripe-Signature header.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
)

// Config is the account setup.
type Config struct {
	SecretKey     string
	WebhookSecret string
	Currency      string // e.g. "usd"; amounts are in the currency's minor unit
	SuccessURL    string
	CancelURL     string
	APIBase       string // override for tests; default https://api.stripe.com
	Tolerance     time.Duration
}

// Gateway is a Stripe client.
type Gateway struct {
	cfg  Config
	http *http.Client
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.SecretKey == "" || cfg.WebhookSecret == "" || cfg.SuccessURL == "" || cfg.CancelURL == "" {
		return nil, fmt.Errorf("stripe: secret_key, webhook_secret, success_url and cancel_url are required")
	}
	if cfg.Currency == "" {
		cfg.Currency = "usd"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.stripe.com"
	}
	if cfg.Tolerance == 0 {
		cfg.Tolerance = 5 * time.Minute
	}
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *Gateway) Name() string { return "stripe" }

// Create makes a Checkout Session and returns its hosted URL.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, plan *domain.Plan, user *domain.User, _ string) (*payment.Checkout, error) {
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", g.cfg.SuccessURL)
	form.Set("cancel_url", g.cfg.CancelURL)
	form.Set("client_reference_id", order.No)
	form.Set("metadata[order_no]", order.No)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", g.cfg.Currency)
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(order.AmountCents, 10))
	form.Set("line_items[0][price_data][product_data][name]", plan.Name)
	if user != nil && user.Email != "" {
		form.Set("customer_email", user.Email)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.APIBase+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", "order-"+order.No)
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stripe: create session: %s: %s", resp.Status, truncate(body))
	}
	var sess struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &sess); err != nil || sess.URL == "" {
		return nil, fmt.Errorf("stripe: bad session response: %s", truncate(body))
	}
	return &payment.Checkout{URL: sess.URL}, nil
}

// Notify verifies the webhook signature and handles checkout.session.completed.
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := VerifySignature(r.Header.Get("Stripe-Signature"), body, g.cfg.WebhookSecret, time.Now(), g.cfg.Tolerance); err != nil {
		return nil, err
	}
	var ev struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string `json:"id"`
				ClientReferenceID string `json:"client_reference_id"`
				PaymentStatus     string `json:"payment_status"`
				PaymentIntent     string `json:"payment_intent"`
				Metadata          struct {
					OrderNo string `json:"order_no"`
				} `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("stripe: decode event: %w", err)
	}
	obj := ev.Data.Object
	orderNo := obj.ClientReferenceID
	if orderNo == "" {
		orderNo = obj.Metadata.OrderNo
	}
	paid := (ev.Type == "checkout.session.completed" || ev.Type == "checkout.session.async_payment_succeeded") && obj.PaymentStatus == "paid"
	ref := obj.PaymentIntent
	if ref == "" {
		ref = obj.ID
	}
	return &payment.Notification{OrderNo: orderNo, GatewayRef: ref, Paid: paid, Response: `{"received":true}`}, nil
}

// VerifySignature checks a Stripe-Signature header ("t=...,v1=...") against
// the raw body: HMAC-SHA256(secret, "<t>.<body>") must match a v1 value and
// the timestamp must be within tolerance.
func VerifySignature(header string, body []byte, secret string, now time.Time, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == "" || len(sigs) == 0 {
		return fmt.Errorf("stripe: malformed signature header")
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("stripe: bad timestamp")
	}
	if d := now.Sub(time.Unix(t, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("stripe: signature timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return fmt.Errorf("stripe: signature mismatch")
}

// SignForTest builds a valid header, for tests and local simulation.
func SignForTest(body []byte, secret string, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "..."
	}
	return string(b)
}
