package stripe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.com/boyang-hu/captain/internal/domain"
)

func TestCreateSession(t *testing.T) {
	var got string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" || r.Header.Get("Authorization") != "Bearer sk_test" {
			http.Error(w, "unauthorized", 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_, _ = w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`))
	}))
	defer api.Close()
	g, err := New(Config{SecretKey: "sk_test", WebhookSecret: "whsec", SuccessURL: "https://p/ok", CancelURL: "https://p/no", APIBase: api.URL})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 1999}, &domain.Plan{Name: "pro"}, &domain.User{Email: "u@test"}, "")
	if err != nil || co.URL != "https://checkout.stripe.com/c/pay/cs_1" {
		t.Fatalf("create: %+v %v", co, err)
	}
	for _, want := range []string{"client_reference_id=O1", "unit_amount%5D=1999", "currency%5D=usd", "name%5D=pro", "customer_email=u%40test"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("form missing %s: %s", want, got)
		}
	}
}

func TestWebhook(t *testing.T) {
	g, _ := New(Config{SecretKey: "sk", WebhookSecret: "whsec", SuccessURL: "a", CancelURL: "b"})
	body := []byte(`{"type":"checkout.session.completed","data":{"object":{"id":"cs_1","client_reference_id":"O1","payment_status":"paid","payment_intent":"pi_1"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", SignForTest(body, "whsec", time.Now()))
	n, err := g.Notify(req)
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "pi_1" {
		t.Fatalf("notify: %+v %v", n, err)
	}
	// Wrong secret, stale timestamp, tampered body.
	req = httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", SignForTest(body, "other", time.Now()))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("wrong secret accepted")
	}
	req = httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", SignForTest(body, "whsec", time.Now().Add(-time.Hour)))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("stale signature accepted")
	}
	tampered := bytes.Replace(body, []byte(`"paid"`), []byte(`"unpaid"`), 1)
	req = httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(tampered))
	req.Header.Set("Stripe-Signature", SignForTest(body, "whsec", time.Now()))
	if _, err := g.Notify(req); err == nil {
		t.Fatal("tampered body accepted")
	}
	// Unpaid event is verified but not paid.
	unpaid := []byte(`{"type":"checkout.session.completed","data":{"object":{"id":"cs_2","client_reference_id":"O2","payment_status":"unpaid"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(unpaid))
	req.Header.Set("Stripe-Signature", SignForTest(unpaid, "whsec", time.Now()))
	if n, err := g.Notify(req); err != nil || n.Paid {
		t.Fatalf("unpaid: %+v %v", n, err)
	}
}
