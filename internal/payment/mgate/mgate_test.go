package mgate

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

func TestCreate(t *testing.T) {
	var got url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gateway/fetch" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		params := map[string]string{}
		for k := range got {
			if k != "sign" {
				params[k] = got.Get(k)
			}
		}
		if got.Get("sign") != Sign(params, "secret") {
			http.Error(w, "bad sign", 400)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"trade_no":"MG1","pay_url":"https://gateway.test/pay/MG1"}}`))
	}))
	defer api.Close()
	g, err := New(Config{URL: api.URL + "/", AppID: "app", AppSecret: "secret", SourceCurrency: "CNY", NotifyURL: "https://p/api/payment/mgate/notify", ReturnURL: "https://p/portal/orders"})
	if err != nil {
		t.Fatal(err)
	}
	co, err := g.Create(context.Background(), &domain.Order{No: "O1", AmountCents: 1500}, &domain.Plan{Name: "pro"}, nil, "")
	if err != nil || co.URL != "https://gateway.test/pay/MG1" {
		t.Fatalf("create: %+v %v", co, err)
	}
	if got.Get("total_amount") != "1500" || got.Get("source_currency") != "CNY" || got.Get("app_id") != "app" {
		t.Fatalf("form: %v", got)
	}
}

func TestNotify(t *testing.T) {
	g, _ := New(Config{URL: "https://gateway.test", AppID: "app", AppSecret: "secret", NotifyURL: "n", ReturnURL: "r"})
	params := map[string]string{"app_id": "app", "out_trade_no": "O1", "trade_no": "MG1", "total_amount": "1500", "status": "paid"}
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	q.Set("sign", Sign(params, "secret"))
	n, err := g.Notify(httptest.NewRequest(http.MethodGet, "/notify?"+q.Encode(), nil))
	if err != nil || !n.Paid || n.OrderNo != "O1" || n.GatewayRef != "MG1" || n.Response != "success" {
		t.Fatalf("notify: %+v %v", n, err)
	}
	// POST form works the same way.
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(q.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if n, err := g.Notify(req); err != nil || !n.Paid || n.AmountCents != 1500 {
		t.Fatalf("post notify: %+v %v", n, err)
	}
	q.Set("total_amount", "1")
	if _, err := g.Notify(httptest.NewRequest(http.MethodGet, "/notify?"+q.Encode(), nil)); err == nil {
		t.Fatal("tampered callback accepted")
	}
	q.Set("total_amount", "1500")
	q.Set("sign", Sign(params, "other"))
	if _, err := g.Notify(httptest.NewRequest(http.MethodGet, "/notify?"+q.Encode(), nil)); err == nil {
		t.Fatal("wrong secret accepted")
	}
}
