package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/boyang-hu/bosun/pkg/agentproto"

	"gitlab.com/boyang-hu/captain/internal/config"
	"gitlab.com/boyang-hu/captain/internal/db"
	"gitlab.com/boyang-hu/captain/internal/http/admin"
	"gitlab.com/boyang-hu/captain/internal/payment"
	"gitlab.com/boyang-hu/captain/internal/payment/epay"
	"gitlab.com/boyang-hu/captain/internal/payment/stripe"
	"gitlab.com/boyang-hu/captain/internal/store"
)

func TestPurchaseFlow(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)

	// Stripe's API is faked; EPay only needs a URL.
	stripeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`))
	}))
	defer stripeAPI.Close()
	ep, _ := epay.New(epay.Config{URL: "https://zpayz.cn", PID: "1000", Key: "k", NotifyURL: "http://test/api/payment/epay/notify", ReturnURL: "http://test/api/payment/epay/return"})
	sp, _ := stripe.New(stripe.Config{SecretKey: "sk", WebhookSecret: "whsec", SuccessURL: "http://test/ok", CancelURL: "http://test/no", APIBase: stripeAPI.URL})
	srv := httptest.NewServer(New(cfg, st, slog.Default(), Options{Gateways: map[string]payment.Gateway{"epay": ep, "stripe": sp}}).Handler())
	defer srv.Close()

	adm := &client{t: t, srv: srv}
	adm.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := adm.do("POST", "/api/admin/plans", map[string]any{"Name": "basic", "PriceCents": 1000, "PeriodDays": 30, "QuotaBytes": 1 << 30}, nil)
	planID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	_, b, _ = adm.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	adm.do("POST", "/api/admin/nodes/"+itoa(int64(node["ID"].(float64)))+"/inbounds", map[string]any{"Tag": "t", "Protocol": "vmess", "Port": 1}, nil)
	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["PairCode"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token

	// Self sign-up, no subscription yet.
	u := &client{t: t, srv: srv}
	code, b, _ := u.do("POST", "/api/portal/register", map[string]string{"Email": "Buyer@Test", "Password": "password123"}, nil)
	if code != 200 {
		t.Fatalf("register: %d %s", code, b)
	}
	me := mustJSON[map[string]any](t, b)
	if me["subscription"] != nil || !strings.HasPrefix(me["subscription_url"].(string), "http://test/sub/") {
		t.Fatalf("me: %v", me)
	}
	if code, _, _ := u.do("POST", "/api/portal/register", map[string]string{"Email": "buyer@test", "Password": "password123"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate email: %d", code)
	}
	_, b, _ = u.do("GET", "/api/portal/plans", nil, nil)
	if !strings.Contains(string(b), `"Name":"basic"`) {
		t.Fatalf("plans: %s", b)
	}

	// EPay order: pay_url is a signed submit.php link.
	_, b, _ = u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "epay"}, nil)
	ord := mustJSON[map[string]any](t, b)
	payURL, _ := url.Parse(ord["pay_url"].(string))
	if payURL.Path != "/submit.php" || payURL.Query().Get("out_trade_no") != ord["order_no"] || payURL.Query().Get("money") != "10.00" {
		t.Fatalf("epay order: %v", ord)
	}
	// Provider notifies (twice: second must be idempotent). Tampered amount is refused.
	notify := func(status, money string) (int, string) {
		cb := map[string]string{"pid": "1000", "name": "basic", "money": money, "out_trade_no": ord["order_no"].(string), "trade_no": "Z1", "trade_status": status, "type": "alipay", "sign_type": "MD5"}
		cb["sign"] = epay.Sign(cb, "k")
		v := url.Values{}
		for k, val := range cb {
			v.Set(k, val)
		}
		c, body, _ := (&client{t: t, srv: srv}).do("GET", "/api/payment/epay/notify?"+v.Encode(), nil, nil)
		return c, string(body)
	}
	if c, body := notify("TRADE_SUCCESS", "0.01"); c != http.StatusBadRequest {
		t.Fatalf("tampered amount accepted: %d %s", c, body)
	}
	if c, body := notify("TRADE_SUCCESS", "10.00"); c != 200 || body != "success" {
		t.Fatalf("notify: %d %s", c, body)
	}
	if c, body := notify("TRADE_SUCCESS", "10.00"); c != 200 || body != "success" {
		t.Fatalf("repeat notify: %d %s", c, body)
	}
	_, b, _ = u.do("GET", "/api/portal/me", nil, nil)
	me = mustJSON[map[string]any](t, b)
	sub := me["subscription"].(map[string]any)
	if sub["usable"] != true || sub["quota_bytes"] != float64(1<<30) {
		t.Fatalf("subscription after payment: %v", sub)
	}
	_, b, _ = u.do("GET", "/api/portal/orders", nil, nil)
	if !strings.Contains(string(b), `"Status":"paid"`) || strings.Count(string(b), `"Status":"paid"`) != 1 {
		t.Fatalf("orders: %s", b)
	}
	// The node now sees the buyer.
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 1 {
		t.Fatalf("agent state after purchase: %+v", stt.Users)
	}

	// Stripe order + webhook.
	_, b, _ = u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "stripe"}, nil)
	ord2 := mustJSON[map[string]any](t, b)
	if !strings.HasPrefix(ord2["pay_url"].(string), "https://checkout.stripe.com/") {
		t.Fatalf("stripe order: %v", ord2)
	}
	body := `{"type":"checkout.session.completed","data":{"object":{"id":"cs_1","client_reference_id":"` + ord2["order_no"].(string) + `","payment_status":"paid","payment_intent":"pi_1"}}}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/payment/stripe/notify", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", stripe.SignForTest([]byte(body), "whsec", time.Now()))
	resp, err := srv.Client().Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("stripe webhook: %v %v", err, resp)
	}
	o2, _ := st.OrderByNo(context.Background(), ord2["order_no"].(string))
	if o2.Status != "paid" || o2.GatewayRef != "pi_1" {
		t.Fatalf("stripe order not settled: %+v", o2)
	}

	// Balance order: insufficient, then topped up by admin.
	if code, _, _ := u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance"}, nil); code != http.StatusPaymentRequired {
		t.Fatalf("insufficient balance: %d", code)
	}
	adm.do("POST", "/api/admin/users/"+itoa(int64(me["id"].(float64)))+"/balance", map[string]any{"DeltaCents": 1500}, nil)
	_, b, _ = u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance"}, nil)
	if ord3 := mustJSON[map[string]any](t, b); ord3["status"] != "paid" {
		t.Fatalf("balance order: %v", ord3)
	}
	_, b, _ = u.do("GET", "/api/portal/me", nil, nil)
	if me = mustJSON[map[string]any](t, b); me["balance_cents"] != float64(500) {
		t.Fatalf("balance after purchase: %v", me["balance_cents"])
	}
	// Unknown gateway.
	if code, _, _ := u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "paypal"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown gateway: %d", code)
	}
}
