// Package btcpay implements BTCPay Server through the Greenfield API: one
// JSON POST creates an invoice with the order number in its metadata, the
// user pays on the BTCPay checkout page, and the webhook is verified with
// HMAC-SHA256 of the raw body (BTCPay-Sig: sha256=<hex>).
package btcpay

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

// Config is the store setup.
type Config struct {
	URL           string // server root, e.g. https://btcpay.example.com
	StoreID       string
	APIKey        string // a user API key with btcpay.store.cancreateinvoice + canviewinvoices
	WebhookSecret string
	Currency      string // invoice currency; default CNY
	RedirectURL   string // after payment
}

// Gateway is a BTCPay client.
type Gateway struct {
	cfg  Config
	http *http.Client
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.URL == "" || cfg.StoreID == "" || cfg.APIKey == "" || cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("btcpay: url, store_id, api_key and webhook_secret are required")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.Currency == "" {
		cfg.Currency = "CNY"
	}
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *Gateway) Name() string { return "btcpay" }

func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func (g *Gateway) invoicesURL() string {
	return g.cfg.URL + "/api/v1/stores/" + g.cfg.StoreID + "/invoices"
}

// Create makes an invoice and returns its checkout link.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, plan *domain.Plan, user *domain.User, _ string) (*payment.Checkout, error) {
	meta := map[string]any{"orderId": order.No, "itemDesc": plan.Name}
	if user != nil && user.Email != "" {
		meta["buyerEmail"] = user.Email
	}
	body, _ := json.Marshal(map[string]any{
		"amount":   money(order.AmountCents),
		"currency": strings.ToUpper(g.cfg.Currency),
		"metadata": meta,
		"checkout": map[string]any{"redirectURL": g.cfg.RedirectURL, "redirectAutomatically": g.cfg.RedirectURL != ""},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.invoicesURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+g.cfg.APIKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("btcpay: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		ID           string `json:"id"`
		CheckoutLink string `json:"checkoutLink"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("btcpay: bad response: %w", err)
	}
	if resp.StatusCode/100 != 2 || out.CheckoutLink == "" {
		return nil, fmt.Errorf("btcpay: create invoice failed: %s", out.Message)
	}
	return &payment.Checkout{URL: out.CheckoutLink}, nil
}

// SignForTest computes the BTCPay-Sig header value for body.
func SignForTest(body []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Notify verifies the webhook. InvoiceSettled is the terminal "paid" event;
// InvoicePaymentSettled is accepted too because BTCPay only sends it once a
// payment is confirmed. The order number comes from the invoice metadata in
// the payload, or from the invoice itself when the payload omits it (older
// servers).
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("btcpay: read body: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(r.Header.Get("BTCPay-Sig"))), []byte(SignForTest(body, g.cfg.WebhookSecret))) != 1 {
		return nil, fmt.Errorf("btcpay: bad signature")
	}
	var ev struct {
		Type      string         `json:"type"`
		InvoiceID string         `json:"invoiceId"`
		StoreID   string         `json:"storeId"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("btcpay: bad json")
	}
	if ev.StoreID != "" && ev.StoreID != g.cfg.StoreID {
		return nil, fmt.Errorf("btcpay: store mismatch")
	}
	orderNo, _ := ev.Metadata["orderId"].(string)
	if orderNo == "" {
		orderNo, err = g.lookupOrder(r.Context(), ev.InvoiceID)
		if err != nil {
			return nil, err
		}
	}
	return &payment.Notification{
		OrderNo:    orderNo,
		GatewayRef: ev.InvoiceID,
		Paid:       ev.Type == "InvoiceSettled" || ev.Type == "InvoicePaymentSettled",
		Response:   "ok",
	}, nil
}

func (g *Gateway) lookupOrder(ctx context.Context, invoiceID string) (string, error) {
	if invoiceID == "" {
		return "", fmt.Errorf("btcpay: event without invoiceId")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.invoicesURL()+"/"+invoiceID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+g.cfg.APIKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("btcpay: %w", err)
	}
	defer resp.Body.Close()
	var inv struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&inv); err != nil {
		return "", fmt.Errorf("btcpay: bad invoice: %w", err)
	}
	orderNo, _ := inv.Metadata["orderId"].(string)
	if orderNo == "" {
		return "", fmt.Errorf("btcpay: invoice without orderId")
	}
	return orderNo, nil
}
