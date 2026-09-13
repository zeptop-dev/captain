// Package alipay implements 支付宝当面付 (face-to-face) through
// alipay.trade.precreate: the gateway returns a payment string the user
// scans with the Alipay app. Requests are signed RSA2 (SHA256withRSA) with
// the merchant private key; the asynchronous notify is verified with the
// Alipay public key.
//
// Browsers cannot do anything useful with the returned "https://qr.alipay.com/..."
// string, so Create points the user at a small page served by this package
// that renders the QR code and links back to the orders page. The page's
// parameters are HMAC-signed so it cannot be used to render arbitrary codes.
package alipay

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
	"github.com/zeptop-dev/captain/internal/payment/epay"
)

// Config is the open-platform app setup.
type Config struct {
	AppID      string
	PrivateKey string // 应用私钥, PEM or bare base64 (PKCS1 or PKCS8)
	PublicKey  string // 支付宝公钥, PEM or bare base64
	Subject    string // bill line shown in the Alipay app; default: the plan name
	NotifyURL  string // absolute
	PageURL    string // absolute URL of the QR page this package serves
	ReturnURL  string // where the QR page's "I have paid" link goes
	APIBase    string // override for tests; default https://openapi.alipay.com/gateway.do
}

// Gateway is an Alipay F2F client.
type Gateway struct {
	cfg  Config
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
	http *http.Client
	mac  []byte // key for signing QR page parameters
}

// New returns a gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.AppID == "" || cfg.PrivateKey == "" || cfg.PublicKey == "" || cfg.NotifyURL == "" || cfg.PageURL == "" {
		return nil, fmt.Errorf("alipay: app_id, private_key, public_key, notify_url and page_url are required")
	}
	priv, err := epay.ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("alipay: private_key: %w", err)
	}
	pub, err := epay.ParsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("alipay: public_key: %w", err)
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://openapi.alipay.com/gateway.do"
	}
	// The page-signing key only needs to be stable for this process and
	// unguessable; deriving it from the private key achieves both.
	mac := sha256.Sum256(priv.D.Bytes())
	return &Gateway{cfg: cfg, priv: priv, pub: pub, http: &http.Client{Timeout: 30 * time.Second}, mac: mac[:]}, nil
}

func (g *Gateway) Name() string { return "alipay" }

// signString joins sorted k=v pairs, skipping sign/sign_type and empties,
// exactly as the Alipay SDKs do.
func signString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// Sign produces the RSA2 signature of params.
func Sign(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	h := sha256.Sum256([]byte(signString(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks an RSA2 signature over params with pub.
func Verify(params map[string]string, sig string, pub *rsa.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("signature is not base64")
	}
	h := sha256.Sum256([]byte(signString(params)))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], raw)
}

func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// Create calls alipay.trade.precreate and returns the QR page URL.
func (g *Gateway) Create(ctx context.Context, order *domain.Order, plan *domain.Plan, _ *domain.User, _ string) (*payment.Checkout, error) {
	subject := g.cfg.Subject
	if subject == "" {
		subject = plan.Name
	}
	biz, _ := json.Marshal(map[string]any{
		"out_trade_no":    order.No,
		"total_amount":    money(order.AmountCents),
		"subject":         subject,
		"timeout_express": "30m",
	})
	params := map[string]string{
		"app_id":      g.cfg.AppID,
		"method":      "alipay.trade.precreate",
		"charset":     "UTF-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  g.cfg.NotifyURL,
		"biz_content": string(biz),
	}
	sig, err := Sign(params, g.priv)
	if err != nil {
		return nil, err
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.APIBase, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alipay: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		R struct {
			Code   string `json:"code"`
			Msg    string `json:"msg"`
			SubMsg string `json:"sub_msg"`
			QRCode string `json:"qr_code"`
		} `json:"alipay_trade_precreate_response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("alipay: bad response: %w", err)
	}
	if out.R.Code != "10000" || out.R.QRCode == "" {
		msg := out.R.SubMsg
		if msg == "" {
			msg = out.R.Msg
		}
		return nil, fmt.Errorf("alipay: precreate failed: %s", msg)
	}
	q := url.Values{"order": {order.No}, "code": {out.R.QRCode}}
	q.Set("sig", g.pageSig(order.No, out.R.QRCode))
	return &payment.Checkout{URL: g.cfg.PageURL + "?" + q.Encode()}, nil
}

func (g *Gateway) pageSig(order, code string) string {
	m := hmac.New(sha256.New, g.mac)
	m.Write([]byte(order + "\x00" + code))
	return hex.EncodeToString(m.Sum(nil))
}

// ServePage renders the QR code page for a checkout produced by Create.
func (g *Gateway) ServePage(w http.ResponseWriter, r *http.Request) {
	order, code, sig := r.URL.Query().Get("order"), r.URL.Query().Get("code"), r.URL.Query().Get("sig")
	if subtle.ConstantTimeCompare([]byte(sig), []byte(g.pageSig(order, code))) != 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, pageHTML, html.EscapeString(order), html.EscapeString(code), html.EscapeString(g.cfg.ReturnURL), html.EscapeString(code))
}

// The QR library is the widely mirrored qrcodejs; the page works without any
// other resource, and the alipays:// link covers phones without a camera
// pointed at the screen.
const pageHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Alipay</title>
<style>body{font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;background:#f4f6fb;margin:0;display:flex;min-height:100vh;align-items:center;justify-content:center}
.card{background:#fff;border-radius:16px;padding:32px;max-width:360px;width:90%%;text-align:center;box-shadow:0 8px 30px rgba(0,0,0,.06)}
h1{font-size:20px;margin:0 0 4px;color:#1677ff}p{color:#666;font-size:14px;margin:8px 0}#qr{display:inline-block;margin:16px 0;padding:12px;background:#fff;border:1px solid #eee;border-radius:12px}
a.btn{display:inline-block;margin-top:12px;padding:10px 20px;border-radius:8px;background:#1677ff;color:#fff;text-decoration:none;font-weight:600}a.open{display:block;margin-top:10px;color:#1677ff;font-size:14px}</style>
<script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script></head>
<body><div class="card"><h1>支付宝扫码付款</h1><p>订单 %s</p><div id="qr"></div>
<p>用支付宝扫描二维码完成付款；付款后订阅自动生效。</p>
<a class="open" href="alipays://platformapi/startapp?saId=10000007&qrcode=%s">手机上打开支付宝付款</a>
<a class="btn" href="%s">我已付款，返回</a></div>
<script>new QRCode(document.getElementById("qr"),{text:"%s",width:200,height:200});</script></body></html>`

// Notify verifies the asynchronous callback (POST form) and reports whether
// the trade succeeded. Alipay expects the literal body "success".
func (g *Gateway) Notify(r *http.Request) (*payment.Notification, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("alipay: bad form")
	}
	params := make(map[string]string, len(r.Form))
	for k := range r.Form {
		params[k] = r.Form.Get(k)
	}
	if params["sign_type"] != "RSA2" {
		return nil, fmt.Errorf("alipay: unsupported sign_type")
	}
	if err := Verify(params, params["sign"], g.pub); err != nil {
		return nil, fmt.Errorf("alipay: bad signature: %w", err)
	}
	if params["app_id"] != g.cfg.AppID {
		return nil, fmt.Errorf("alipay: app_id mismatch")
	}
	status := params["trade_status"]
	return &payment.Notification{
		OrderNo:     params["out_trade_no"],
		GatewayRef:  params["trade_no"],
		Paid:        status == "TRADE_SUCCESS" || status == "TRADE_FINISHED",
		AmountCents: payment.ParseMoney(params["total_amount"]),
		Response:    "success",
	}, nil
}
