package alipay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func keys(t *testing.T) (priv string, pub string, k *rsa.PrivateKey) {
	t.Helper()
	k, _ = rsa.GenerateKey(rand.Reader, 2048)
	priv = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pub = base64.StdEncoding.EncodeToString(der) // bare base64 as shown in the Alipay console
	return
}

func TestCreateAndPage(t *testing.T) {
	priv, pub, k := keys(t)
	var got url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form
		// The request must carry a valid RSA2 signature over its own params.
		params := map[string]string{}
		for key := range r.Form {
			params[key] = r.Form.Get(key)
		}
		if err := Verify(params, params["sign"], &k.PublicKey); err != nil {
			http.Error(w, "bad sign", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": map[string]any{"code": "10000", "msg": "Success", "out_trade_no": "O1", "qr_code": "https://qr.alipay.com/bax0001"}})
	}))
	defer api.Close()
	g, err := New(Config{AppID: "2021", PrivateKey: priv, PublicKey: pub, NotifyURL: "https://p/api/payment/alipay/notify", PageURL: "https://p/api/payment/alipay/page", ReturnURL: "https://p/portal/orders", APIBase: api.URL})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 1050}, &domain.Plan{Name: "pro"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("method") != "alipay.trade.precreate" || got.Get("sign_type") != "RSA2" || !strings.Contains(got.Get("biz_content"), `"total_amount":"10.50"`) || !strings.Contains(got.Get("biz_content"), `"subject":"pro"`) {
		t.Fatalf("request: %v", got)
	}
	u, _ := url.Parse(co.URL)
	if u.Query().Get("code") != "https://qr.alipay.com/bax0001" || u.Query().Get("order") != "O1" {
		t.Fatalf("checkout url %s", co.URL)
	}
	// The QR page renders for a signed URL and refuses a tampered one.
	rec := httptest.NewRecorder()
	g.ServePage(rec, httptest.NewRequest(http.MethodGet, co.URL, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "qr.alipay.com/bax0001") {
		t.Fatalf("page: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	g.ServePage(rec, httptest.NewRequest(http.MethodGet, strings.Replace(co.URL, "bax0001", "evil", 1), nil))
	if rec.Code != 400 {
		t.Fatalf("tampered page served: %d", rec.Code)
	}
}

func TestNotify(t *testing.T) {
	priv, pub, k := keys(t)
	g, _ := New(Config{AppID: "2021", PrivateKey: priv, PublicKey: pub, NotifyURL: "n", PageURL: "p"})
	params := map[string]string{"app_id": "2021", "out_trade_no": "O1", "trade_no": "2026091322001", "trade_status": "TRADE_SUCCESS", "total_amount": "10.50", "sign_type": "RSA2", "charset": "utf-8"}
	sig, _ := Sign(params, k) // Alipay signs with its own key; in the test the "Alipay key" is ours
	form := url.Values{}
	for key, v := range params {
		form.Set(key, v)
	}
	form.Set("sign", sig)
	post := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	req := post()
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "2026091322001" || n.Response != "success" || n.AmountCents != 1050 {
		t.Fatalf("notify: %+v %v", n, err)
	}
	// Tampered amount fails the signature.
	form.Set("total_amount", "0.01")
	if _, err := g.Notify(post()); err == nil {
		t.Fatal("tampered callback accepted")
	}
	// A different signer is rejected.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	form.Set("total_amount", "10.50")
	sig, _ = Sign(params, other)
	form.Set("sign", sig)
	if _, err := g.Notify(post()); err == nil {
		t.Fatal("foreign signature accepted")
	}
}
