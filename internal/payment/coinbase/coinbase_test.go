package coinbase

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestCreateCharge(t *testing.T) {
	var got map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/charges" || r.Header.Get("X-CC-Api-Key") != "key" {
			http.Error(w, "unauthorized", 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"data":{"id":"c1","code":"ABCDEF","hosted_url":"https://commerce.coinbase.com/charges/ABCDEF"}}`))
	}))
	defer api.Close()
	g, err := New(Config{APIKey: "key", WebhookSecret: "whs", Currency: "usd", APIBase: api.URL})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 1999}, &domain.Plan{Name: "pro"}, nil, "")
	if err != nil || co.URL != "https://commerce.coinbase.com/charges/ABCDEF" {
		t.Fatalf("create: %+v %v", co, err)
	}
	lp := got["local_price"].(map[string]any)
	if lp["amount"] != "19.99" || lp["currency"] != "USD" || got["metadata"].(map[string]any)["order_no"] != "O1" {
		t.Fatalf("charge body: %v", got)
	}
}

func TestWebhook(t *testing.T) {
	g, _ := New(Config{APIKey: "key", WebhookSecret: "whs"})
	body := []byte(`{"event":{"id":"ev1","type":"charge:confirmed","data":{"code":"ABCDEF","metadata":{"order_no":"O1"}}}}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("X-CC-Webhook-Signature", SignForTest(body, "whs"))
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "ABCDEF" {
		t.Fatalf("notify: %+v %v", n, err)
	}
	pending := []byte(strings.Replace(string(body), "charge:confirmed", "charge:pending", 1))
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(pending))
	req.Header.Set("X-CC-Webhook-Signature", SignForTest(pending, "whs"))
	if n, err := g.Notify(req); err != nil || n.Paid {
		t.Fatalf("pending treated as paid: %+v %v", n, err)
	}
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("X-CC-Webhook-Signature", SignForTest(body, "other"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("wrong secret accepted")
	}
	tampered := []byte(strings.Replace(string(body), "O1", "O2", 1))
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(tampered))
	req.Header.Set("X-CC-Webhook-Signature", SignForTest(body, "whs"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("tampered body accepted")
	}
}
