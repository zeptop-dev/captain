// Package coinpayments implements CoinPayments through its merchant API:
// create_transaction (form POST, HMAC-SHA512 of the body with the private
// API key in the HMAC header) returns a hosted checkout URL; the IPN callback
// is verified with HMAC-SHA512 of the raw body using the IPN secret.
package coinpayments

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"crypto/subtle"
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

// Config is the merchant setup.
type Config struct {
	MerchantID string
	PublicKey  string // API public key
	PrivateKey string // API private key
	IPNSecret  string
	Currency   string // price currency (currency1), e.g. USD or CNY; default USD
	Currency2  string // what the buyer pays with; default LTCT? no: default = same as Currency (buyer picks on checkout)
	NotifyURL  string // IPN URL, absolute
	ReturnURL  string // success_url after paying
	CancelURL  string
	APIBase    string // override for tests; default https://www.coinpayments.net/api.php
}

// Gateway is a CoinPayments client.
type Gateway struct {
	cfg  Config
	http *http.Client
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.MerchantID == "" || cfg.PublicKey == "" || cfg.PrivateKey == "" || cfg.IPNSecret == "" || cfg.NotifyURL == "" {
		return nil, fmt.Errorf("coinpayments: merchant_id, public_key, private_key, ipn_secret and notify_url are required")
	}
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	if cfg.Currency2 == "" {
		cfg.Currency2 = cfg.Currency
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://www.coinpayments.net/api.php"
	}
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *Gateway) Name() string { return "coinpayments" }

func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func hmac512(body []byte, key string) string {
	m := hmac.New(sha512.New, []byte(key))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Create calls create_transaction and returns the hosted checkout URL.
// currency2 equal to currency1 lets the buyer choose the coin on the
// CoinPayments checkout page.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, plan *domain.Plan, user *domain.User, _ string) (*payment.Checkout, error) {
	form := url.Values{}
	form.Set("version", "1")
	form.Set("cmd", "create_transaction")
	form.Set("key", g.cfg.PublicKey)
	form.Set("format", "json")
	form.Set("amount", money(order.AmountCents))
	form.Set("currency1", strings.ToUpper(g.cfg.Currency))
	form.Set("currency2", strings.ToUpper(g.cfg.Currency2))
	form.Set("item_name", plan.Name)
	form.Set("item_number", order.No)
	form.Set("invoice", order.No)
	form.Set("ipn_url", g.cfg.NotifyURL)
	if g.cfg.ReturnURL != "" {
		form.Set("success_url", g.cfg.ReturnURL)
	}
	if g.cfg.CancelURL != "" {
		form.Set("cancel_url", g.cfg.CancelURL)
	}
	if user != nil && user.Email != "" {
		form.Set("buyer_email", user.Email)
	}
	body := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.APIBase, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HMAC", hmac512([]byte(body), g.cfg.PrivateKey))
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coinpayments: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Error  string `json:"error"`
		Result struct {
			TxnID       string `json:"txn_id"`
			CheckoutURL string `json:"checkout_url"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("coinpayments: bad response: %w", err)
	}
	if out.Error != "ok" || out.Result.CheckoutURL == "" {
		return nil, fmt.Errorf("coinpayments: create_transaction failed: %s", out.Error)
	}
	return &payment.Checkout{URL: out.Result.CheckoutURL}, nil
}

// SignIPNForTest computes the HMAC header for an IPN body.
func SignIPNForTest(body []byte, ipnSecret string) string { return hmac512(body, ipnSecret) }

// Notify verifies an IPN. Status 100+ is complete, 2 is "queued for
// nightly payout" which CoinPayments documents as paid as well; negative
// statuses are failures. Any accepted IPN is answered with "IPN OK".
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("coinpayments: read body: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(r.Header.Get("HMAC"))), []byte(hmac512(body, g.cfg.IPNSecret))) != 1 {
		return nil, fmt.Errorf("coinpayments: bad signature")
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("coinpayments: bad form")
	}
	if form.Get("merchant") != g.cfg.MerchantID {
		return nil, fmt.Errorf("coinpayments: merchant mismatch")
	}
	if form.Get("ipn_type") != "" && form.Get("ipn_type") != "api" && form.Get("ipn_type") != "simple" && form.Get("ipn_type") != "button" {
		return nil, fmt.Errorf("coinpayments: unexpected ipn_type %q", form.Get("ipn_type"))
	}
	status, _ := strconv.Atoi(form.Get("status"))
	orderNo := form.Get("item_number")
	if orderNo == "" {
		orderNo = form.Get("invoice")
	}
	if orderNo == "" {
		return nil, fmt.Errorf("coinpayments: IPN without order number")
	}
	return &payment.Notification{
		OrderNo:     orderNo,
		GatewayRef:  form.Get("txn_id"),
		Paid:        status >= 100 || status == 2,
		AmountCents: payment.ParseMoney(form.Get("amount1")), // amount1 is in currency1, the order currency
		Response:    "IPN OK",
	}, nil
}
