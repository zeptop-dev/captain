package coinpayments

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestCreateTransaction(t *testing.T) {
	var got url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		// The API authenticates the body with the private key.
		if r.Header.Get("HMAC") != hmac512(b, "priv") {
			http.Error(w, "bad hmac", 401)
			return
		}
		got, _ = url.ParseQuery(string(b))
		_, _ = w.Write([]byte(`{"error":"ok","result":{"txn_id":"CPTX1","checkout_url":"https://www.coinpayments.net/index.php?cmd=checkout&id=CPTX1"}}`))
	}))
	defer api.Close()
	g, err := New(Config{MerchantID: "m1", PublicKey: "pub", PrivateKey: "priv", IPNSecret: "ipn", Currency: "usd", NotifyURL: "https://p/api/payment/coinpayments/notify", APIBase: api.URL})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 1234}, &domain.Plan{Name: "pro"}, &domain.User{Email: "u@test"}, "")
	if err != nil || !strings.Contains(co.URL, "id=CPTX1") {
		t.Fatalf("create: %+v %v", co, err)
	}
	if got.Get("cmd") != "create_transaction" || got.Get("amount") != "12.34" || got.Get("currency1") != "USD" || got.Get("item_number") != "O1" || got.Get("key") != "pub" {
		t.Fatalf("form: %v", got)
	}
}

func TestIPN(t *testing.T) {
	g, _ := New(Config{MerchantID: "m1", PublicKey: "pub", PrivateKey: "priv", IPNSecret: "ipn", NotifyURL: "n"})
	form := url.Values{"ipn_type": {"api"}, "merchant": {"m1"}, "status": {"100"}, "txn_id": {"CPTX1"}, "item_number": {"O1"}, "amount1": {"12.34"}}
	body := []byte(form.Encode())
	post := func(b []byte, secret string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HMAC", SignIPNForTest(b, secret))
		return req
	}
	n, err := g.Notify(post(body, "ipn"))
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "CPTX1" || n.Response != "IPN OK" || n.AmountCents != 1234 {
		t.Fatalf("ipn: %+v %v", n, err)
	}
	form.Set("status", "0")
	if n, err := g.Notify(post([]byte(form.Encode()), "ipn")); err != nil || n.Paid {
		t.Fatalf("pending treated as paid: %+v %v", n, err)
	}
	form.Set("status", "2")
	if n, err := g.Notify(post([]byte(form.Encode()), "ipn")); err != nil || !n.Paid {
		t.Fatalf("status 2 should be paid: %+v %v", n, err)
	}
	if _, err := g.Notify(post(body, "other")); err == nil {
		t.Fatal("wrong secret accepted")
	}
	tampered := strings.Replace(string(body), "item_number=O1", "item_number=O2", 1)
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(tampered))
	req.Header.Set("HMAC", SignIPNForTest(body, "ipn"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("tampered body accepted")
	}
	form.Set("merchant", "m2")
	if _, err := g.Notify(post([]byte(form.Encode()), "ipn")); err == nil {
		t.Fatal("foreign merchant accepted")
	}
}
