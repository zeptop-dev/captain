// Package epay implements the 易支付 interface in both versions:
//
//   - V1: MD5 signature, submit.php page jump, GET callbacks (z-pay.cn and
//     most compatible providers).
//   - V2: SHA256WithRSA, /api/pay/submit page jump, timestamped requests,
//     callbacks verified with the platform's public key (ezfp.cn).
//
// The signature string is the same in both: non-empty parameters except
// sign and sign_type, sorted by key, joined as k=v&k=v.
package epay

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
)

// Config is the merchant setup.
type Config struct {
	Version   string // "v1" (default) or "v2"
	URL       string // gateway base URL, e.g. https://zpayz.cn or https://www.ezfp.cn
	PID       string
	Key       string // v1 merchant key
	NotifyURL string // absolute, no query string
	ReturnURL string // absolute, no query string
	Type      string // "alipay" or "wxpay"; "" lets the cashier page choose

	// V2 keys, PEM or bare base64 as shown in the merchant console.
	MerchantPrivateKey string
	PlatformPublicKey  string
}

// Gateway is an EPay client.
type Gateway struct {
	cfg  Config
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.URL == "" || cfg.PID == "" || cfg.NotifyURL == "" || cfg.ReturnURL == "" {
		return nil, fmt.Errorf("epay: url, pid, notify_url and return_url are required")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	g := &Gateway{cfg: cfg}
	switch cfg.Version {
	case "", "v1":
		g.cfg.Version = "v1"
		if cfg.Key == "" {
			return nil, fmt.Errorf("epay v1: key is required")
		}
	case "v2":
		var err error
		if g.priv, err = ParsePrivateKey(cfg.MerchantPrivateKey); err != nil {
			return nil, fmt.Errorf("epay v2: merchant_private_key: %w", err)
		}
		if g.pub, err = ParsePublicKey(cfg.PlatformPublicKey); err != nil {
			return nil, fmt.Errorf("epay v2: platform_public_key: %w", err)
		}
	default:
		return nil, fmt.Errorf("epay: version must be v1 or v2")
	}
	return g, nil
}

func (g *Gateway) Name() string { return "epay" }

// signString builds the canonical string both versions sign.
func signString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k + "=" + params[k])
	}
	return b.String()
}

// Sign is the V1 rule: md5(signString + key), lowercase hex.
func Sign(params map[string]string, key string) string {
	sum := md5.Sum([]byte(signString(params) + key))
	return hex.EncodeToString(sum[:])
}

// SignRSA is the V2 rule: base64(SHA256WithRSA(signString)).
func SignRSA(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	h := sha256.Sum256([]byte(signString(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyRSA checks a V2 signature with the platform public key.
func VerifyRSA(params map[string]string, sig string, pub *rsa.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("epay: signature is not base64")
	}
	h := sha256.Sum256([]byte(signString(params)))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], raw)
}

// ParsePrivateKey accepts PKCS8 or PKCS1, PEM or bare base64.
func ParsePrivateKey(s string) (*rsa.PrivateKey, error) {
	der, err := keyDER(s)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, fmt.Errorf("not an RSA key")
	}
	return x509.ParsePKCS1PrivateKey(der)
}

// ParsePublicKey accepts PKIX or PKCS1, PEM or bare base64.
func ParsePublicKey(s string) (*rsa.PublicKey, error) {
	der, err := keyDER(s)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rk, ok := k.(*rsa.PublicKey); ok {
			return rk, nil
		}
		return nil, fmt.Errorf("not an RSA key")
	}
	return x509.ParsePKCS1PublicKey(der)
}

func keyDER(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty key")
	}
	if strings.HasPrefix(s, "-----") {
		block, _ := pem.Decode([]byte(s))
		if block == nil {
			return nil, fmt.Errorf("bad PEM")
		}
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
}

func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// Create returns the page-jump URL with signed query parameters; the user's
// browser opens it and the provider shows the cashier.
func (g *Gateway) Create(_ context.Context, order *domain.Order, plan *domain.Plan, _ *domain.User, _ string) (*payment.Checkout, error) {
	// Most 易支付 clones (zpayz.cn among them) refuse a submit without a
	// type; alipay is the channel every merchant has.
	typ := g.cfg.Type
	if typ == "" {
		typ = "alipay"
	}
	params := map[string]string{
		"pid":          g.cfg.PID,
		"type":         typ,
		"out_trade_no": order.No,
		"notify_url":   g.cfg.NotifyURL,
		"return_url":   g.cfg.ReturnURL,
		"name":         plan.Name,
		"money":        money(order.AmountCents),
	}
	var path string
	switch g.cfg.Version {
	case "v1":
		path = "/submit.php"
		params["sign_type"] = "MD5"
		params["sign"] = Sign(params, g.cfg.Key)
	case "v2":
		path = "/api/pay/submit"
		params["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		params["sign_type"] = "RSA"
		sig, err := SignRSA(params, g.priv)
		if err != nil {
			return nil, err
		}
		params["sign"] = sig
	}
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	return &payment.Checkout{URL: g.cfg.URL + path + "?" + q.Encode()}, nil
}

// Notify verifies a callback (notify_url or return_url carry the same GET
// parameters) and reports whether the order is paid.
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	params := callbackParams(r)
	if params["pid"] != g.cfg.PID {
		return nil, fmt.Errorf("epay: pid mismatch")
	}
	switch g.cfg.Version {
	case "v1":
		want := Sign(params, g.cfg.Key)
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(params["sign"])), []byte(want)) != 1 {
			return nil, fmt.Errorf("epay: bad signature")
		}
	case "v2":
		if err := VerifyRSA(params, params["sign"], g.pub); err != nil {
			return nil, fmt.Errorf("epay: bad signature: %w", err)
		}
		if ts, err := strconv.ParseInt(params["timestamp"], 10, 64); err != nil || absDur(time.Since(time.Unix(ts, 0))) > 10*time.Minute {
			return nil, fmt.Errorf("epay: timestamp missing or outside tolerance")
		}
	}
	return &payment.Notification{
		OrderNo:    params["out_trade_no"],
		GatewayRef: params["trade_no"],
		Paid:       params["trade_status"] == "TRADE_SUCCESS",
		Response:   "success",
	}, nil
}

func callbackParams(r *http.Request) map[string]string {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		q = r.Form
	}
	params := make(map[string]string, len(q))
	for k := range q {
		params[k] = q.Get(k)
	}
	return params
}

// VerifyAmount checks the callback amount against the order.
func VerifyAmount(r *http.Request, amountCents int64) bool {
	return callbackParams(r)["money"] == money(amountCents)
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
