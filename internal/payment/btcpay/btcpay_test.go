package btcpay

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

func server(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token apikey" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/stores/S1/invoices":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got)
			_, _ = w.Write([]byte(`{"id":"INV1","checkoutLink":"https://btcpay.test/i/INV1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/stores/S1/invoices/INV1":
			_, _ = w.Write([]byte(`{"id":"INV1","metadata":{"orderId":"O1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &got
}

func TestCreateInvoice(t *testing.T) {
	srv, got := server(t)
	defer srv.Close()
	g, err := New(Config{URL: srv.URL + "/", StoreID: "S1", APIKey: "apikey", WebhookSecret: "whs", RedirectURL: "https://p/portal/orders"})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 2500}, &domain.Plan{Name: "pro"}, &domain.User{Email: "u@test"}, "")
	if err != nil || co.URL != "https://btcpay.test/i/INV1" {
		t.Fatalf("create: %+v %v", co, err)
	}
	if (*got)["amount"] != "25.00" || (*got)["currency"] != "CNY" || (*got)["metadata"].(map[string]any)["orderId"] != "O1" {
		t.Fatalf("invoice body: %v", *got)
	}
}

func TestWebhook(t *testing.T) {
	srv, _ := server(t)
	defer srv.Close()
	g, _ := New(Config{URL: srv.URL, StoreID: "S1", APIKey: "apikey", WebhookSecret: "whs"})
	body := []byte(`{"type":"InvoiceSettled","invoiceId":"INV1","storeId":"S1","metadata":{"orderId":"O1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("BTCPay-Sig", SignForTest(body, "whs"))
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "INV1" {
		t.Fatalf("notify: %+v %v", n, err)
	}
	// Payload without metadata: the invoice is fetched.
	body2 := []byte(`{"type":"InvoiceSettled","invoiceId":"INV1","storeId":"S1"}`)
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body2))
	req.Header.Set("BTCPay-Sig", SignForTest(body2, "whs"))
	if n, err := g.Notify(req); err != nil || n.OrderNo != "O1" {
		t.Fatalf("lookup: %+v %v", n, err)
	}
	// Non-terminal event is not paid.
	body3 := []byte(strings.Replace(string(body), "InvoiceSettled", "InvoiceProcessing", 1))
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body3))
	req.Header.Set("BTCPay-Sig", SignForTest(body3, "whs"))
	if n, err := g.Notify(req); err != nil || n.Paid {
		t.Fatalf("processing treated as paid: %+v %v", n, err)
	}
	// Bad secret, tampered body, wrong store.
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("BTCPay-Sig", SignForTest(body, "nope"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("wrong secret accepted")
	}
	tampered := []byte(strings.Replace(string(body), "O1", "O9", 1))
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(tampered))
	req.Header.Set("BTCPay-Sig", SignForTest(body, "whs"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("tampered body accepted")
	}
	other := []byte(strings.Replace(string(body), `"S1"`, `"S2"`, 1))
	req = httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(other))
	req.Header.Set("BTCPay-Sig", SignForTest(other, "whs"))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("foreign store accepted")
	}
}
