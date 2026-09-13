// Package mgate implements the MGate crypto gateway as Xboard's plugin does:
// POST <url>/v1/gateway/fetch with the order fields sorted by key and
// sign = md5(query + app_secret); the response carries pay_url. The callback
// (GET or POST form) carries the same kind of signature over all fields
// except sign, and is answered with "success".
//
// total_amount is passed in cents, exactly as Xboard sends it.
package mgate

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
)

// Config is the app setup.
type Config struct {
	URL            string // API root, e.g. https://gateway.example.com
	AppID          string
	AppSecret      string
	SourceCurrency string // e.g. CNY; empty = gateway default
	NotifyURL      string // absolute
	ReturnURL      string // absolute
}

// Gateway is an MGate client.
type Gateway struct {
	cfg  Config
	http *http.Client
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.URL == "" || cfg.AppID == "" || cfg.AppSecret == "" || cfg.NotifyURL == "" || cfg.ReturnURL == "" {
		return nil, fmt.Errorf("mgate: url, app_id, app_secret, notify_url and return_url are required")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *Gateway) Name() string { return "mgate" }

// query encodes params sorted by key the way PHP's http_build_query does
// after ksort (RFC 1738: spaces become '+').
func query(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	return strings.Join(parts, "&")
}

// Sign is md5(sorted query + secret), lowercase hex.
func Sign(params map[string]string, secret string) string {
	sum := md5.Sum([]byte(query(params) + secret))
	return hex.EncodeToString(sum[:])
}

// Create registers the order and returns the gateway's payment page.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, _ *domain.Plan, _ *domain.User, _ string) (*payment.Checkout, error) {
	params := map[string]string{
		"app_id":       g.cfg.AppID,
		"out_trade_no": order.No,
		"total_amount": strconv.FormatInt(order.AmountCents, 10),
		"notify_url":   g.cfg.NotifyURL,
		"return_url":   g.cfg.ReturnURL,
	}
	if g.cfg.SourceCurrency != "" {
		params["source_currency"] = g.cfg.SourceCurrency
	}
	params["sign"] = Sign(params, g.cfg.AppSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.URL+"/v1/gateway/fetch", strings.NewReader(query(params)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "MGate")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mgate: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Message string              `json:"message"`
		Errors  map[string][]string `json:"errors"`
		Data    struct {
			TradeNo string `json:"trade_no"`
			PayURL  string `json:"pay_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("mgate: bad response: %w", err)
	}
	if resp.StatusCode/100 != 2 || out.Data.TradeNo == "" || out.Data.PayURL == "" {
		msg := out.Message
		for _, v := range out.Errors {
			if len(v) > 0 {
				msg = v[0]
				break
			}
		}
		return nil, fmt.Errorf("mgate: create failed: %s", msg)
	}
	return &payment.Checkout{URL: out.Data.PayURL}, nil
}

// Notify verifies the callback signature and reports the order as paid.
// MGate only calls back for completed payments, matching Xboard's plugin,
// which treats every verified callback as success.
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return nil, fmt.Errorf("mgate: bad form")
		}
		q = r.Form
	}
	params := make(map[string]string, len(q))
	for k := range q {
		if k != "sign" {
			params[k] = q.Get(k)
		}
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(q.Get("sign"))), []byte(Sign(params, g.cfg.AppSecret))) != 1 {
		return nil, fmt.Errorf("mgate: bad signature")
	}
	if params["app_id"] != "" && params["app_id"] != g.cfg.AppID {
		return nil, fmt.Errorf("mgate: app_id mismatch")
	}
	if params["out_trade_no"] == "" {
		return nil, fmt.Errorf("mgate: callback without out_trade_no")
	}
	amount, _ := strconv.ParseInt(params["total_amount"], 10, 64) // cents, when the gateway echoes it
	return &payment.Notification{
		OrderNo:     params["out_trade_no"],
		GatewayRef:  params["trade_no"],
		Paid:        true,
		AmountCents: amount,
		Response:    "success",
	}, nil
}
