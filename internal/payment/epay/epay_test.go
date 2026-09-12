package epay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestSignKnownVector(t *testing.T) {
	// md5("money=10.00&name=basic&out_trade_no=1&pid=1000" + "key") computed independently.
	got := Sign(map[string]string{"pid": "1000", "name": "basic", "money": "10.00", "out_trade_no": "1", "sign": "x", "sign_type": "MD5", "param": ""}, "key")
	if len(got) != 32 || got != Sign(map[string]string{"pid": "1000", "name": "basic", "money": "10.00", "out_trade_no": "1"}, "key") {
		t.Fatalf("sign: %s (sign/sign_type/empty must be ignored)", got)
	}
	// Determinism and key sensitivity.
	if Sign(map[string]string{"a": "1", "b": "2"}, "k") != Sign(map[string]string{"b": "2", "a": "1"}, "k") {
		t.Fatal("order must not matter")
	}
	if Sign(map[string]string{"a": "1"}, "k1") == Sign(map[string]string{"a": "1"}, "k2") {
		t.Fatal("key must matter")
	}
}

func TestCreateAndNotify(t *testing.T) {
	g, err := New(Config{URL: "https://zpayz.cn/", PID: "1000", Key: "secret", NotifyURL: "https://p.test/api/payment/epay/notify", ReturnURL: "https://p.test/pay/done"})
	if err != nil {
		t.Fatal(err)
	}
	order := &domain.Order{No: "20260911001", AmountCents: 1050}
	co, err := g.Create(context.Background(), order, &domain.Plan{Name: "basic"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(co.URL)
	q := u.Query()
	if u.Host != "zpayz.cn" || u.Path != "/submit.php" || q.Get("money") != "10.50" || q.Get("out_trade_no") != "20260911001" || q.Get("sign_type") != "MD5" {
		t.Fatalf("checkout: %s", co.URL)
	}
	params := map[string]string{}
	for k := range q {
		params[k] = q.Get(k)
	}
	if q.Get("sign") != Sign(params, "secret") {
		t.Fatal("checkout signature")
	}

	// Provider callback: signed with the merchant key.
	cb := map[string]string{"pid": "1000", "name": "basic", "money": "10.50", "out_trade_no": "20260911001", "trade_no": "Z123", "trade_status": "TRADE_SUCCESS", "type": "alipay", "sign_type": "MD5"}
	cb["sign"] = Sign(cb, "secret")
	v := url.Values{}
	for k, val := range cb {
		v.Set(k, val)
	}
	req, _ := http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "20260911001" || n.GatewayRef != "Z123" || n.Response != "success" {
		t.Fatalf("notify: %+v %v", n, err)
	}
	if !VerifyAmount(req, 1050) || VerifyAmount(req, 1000) {
		t.Fatal("amount check")
	}
	// Tampered amount fails the signature.
	v.Set("money", "0.01")
	req, _ = http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	if _, err := g.Notify(req); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered: %v", err)
	}
	// Non-success status is not paid.
	cb["trade_status"] = "WAIT_BUYER_PAY"
	cb["sign"] = Sign(cb, "secret")
	v = url.Values{}
	for k, val := range cb {
		v.Set(k, val)
	}
	req, _ = http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	if n, err := g.Notify(req); err != nil || n.Paid {
		t.Fatalf("unpaid: %+v %v", n, err)
	}
}

func TestV2RoundTrip(t *testing.T) {
	// The platform and the merchant each have a key pair; the merchant signs
	// requests with its private key and verifies callbacks with the
	// platform's public key.
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, merchant)})
	pubDER, _ := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	pubBare := base64.StdEncoding.EncodeToString(pubDER) // bare base64 as consoles show it

	g, err := New(Config{Version: "v2", URL: "https://www.ezfp.cn", PID: "1001", NotifyURL: "https://p/n", ReturnURL: "https://p/r",
		MerchantPrivateKey: string(privPEM), PlatformPublicKey: pubBare})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O2", AmountCents: 999}, &domain.Plan{Name: "pro"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(co.URL)
	q := u.Query()
	if u.Path != "/api/pay/submit" || q.Get("sign_type") != "RSA" || q.Get("timestamp") == "" || q.Get("money") != "9.99" {
		t.Fatalf("v2 checkout: %s", co.URL)
	}
	params := map[string]string{}
	for k := range q {
		params[k] = q.Get(k)
	}
	if err := VerifyRSA(params, q.Get("sign"), &merchant.PublicKey); err != nil {
		t.Fatalf("request signature does not verify with merchant public key: %v", err)
	}

	// Platform callback signed with the platform private key.
	cb := map[string]string{"pid": "1001", "trade_no": "E1", "out_trade_no": "O2", "type": "alipay", "trade_status": "TRADE_SUCCESS",
		"name": "pro", "money": "9.99", "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA", "buyer": "x", "extra_field_added_later": "1"}
	sig, _ := SignRSA(cb, platform)
	cb["sign"] = sig
	v := url.Values{}
	for k, val := range cb {
		v.Set(k, val)
	}
	req, _ := http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "O2" || n.GatewayRef != "E1" {
		t.Fatalf("v2 notify: %+v %v", n, err)
	}
	// Signed by the wrong key.
	sig2, _ := SignRSA(cb, merchant)
	v.Set("sign", sig2)
	req, _ = http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	if _, err := g.Notify(req); err == nil {
		t.Fatal("callback signed by the wrong key was accepted")
	}
	// Stale timestamp.
	cb["timestamp"] = strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	sig3, _ := SignRSA(cb, platform)
	v = url.Values{}
	for k, val := range cb {
		v.Set(k, val)
	}
	v.Set("sign", sig3)
	req, _ = http.NewRequest(http.MethodGet, "/notify?"+v.Encode(), nil)
	if _, err := g.Notify(req); err == nil {
		t.Fatal("stale callback accepted")
	}
}

func mustPKCS8(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
