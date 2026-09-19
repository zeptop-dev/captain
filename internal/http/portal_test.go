package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/telegram"
)

// portalRig is the setup of the customer-facing tests below: a panel, an
// admin session to arrange things, and the store to look behind the API.
type portalRig struct {
	t     *testing.T
	st    *store.Store
	db    *sql.DB
	srv   *httptest.Server
	admin *client
}

// newPortalRig starts a panel. seed runs against the store before the
// server exists, for settings the server caches (mail, Telegram).
func newPortalRig(t *testing.T, registration bool, seed ...func(*store.Store)) *portalRig {
	t.Helper()
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	cfg.Portal.Registration = registration
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	if err := st.CreateUser(context.Background(), adminUser); err != nil {
		t.Fatal(err)
	}
	for _, f := range seed {
		f(st)
	}
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	t.Cleanup(srv.Close)
	p := &portalRig{t: t, st: st, db: conn, srv: srv, admin: &client{t: t, srv: srv}}
	if code, b, _ := p.admin.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != http.StatusOK {
		t.Fatalf("admin login: %d %s", code, b)
	}
	return p
}

// customer creates an active account and signs it in through the portal.
func (p *portalRig) customer(email string) (*domain.User, *client) {
	p.t.Helper()
	u, err := admin.NewUser(email, "password123", "user")
	if err != nil {
		p.t.Fatal(err)
	}
	if err := p.st.CreateUser(context.Background(), u); err != nil {
		p.t.Fatal(err)
	}
	c := &client{t: p.t, srv: p.srv}
	if code, b, _ := c.do("POST", "/api/portal/login", map[string]string{"Email": email, "Password": "password123"}, nil); code != http.StatusOK {
		p.t.Fatalf("login %s: %d %s", email, code, b)
	}
	return u, c
}

func (p *portalRig) reload(id int64) *domain.User {
	p.t.Helper()
	u, err := p.st.UserByID(context.Background(), id)
	if err != nil {
		p.t.Fatal(err)
	}
	return u
}

// plan creates a plan through the admin API and returns its id.
func (p *portalRig) plan(fields map[string]any) int64 {
	p.t.Helper()
	code, b, _ := p.admin.do("POST", "/api/admin/plans", fields, nil)
	if code != http.StatusOK {
		p.t.Fatalf("create plan: %d %s", code, b)
	}
	return int64(mustJSON[map[string]any](p.t, b)["ID"].(float64))
}

func (p *portalRig) credit(userID, cents int64) {
	p.t.Helper()
	if err := p.st.AdjustBalance(context.Background(), userID, cents); err != nil {
		p.t.Fatal(err)
	}
}

// subs returns the subscriptions /me shows the customer.
func subs(t *testing.T, c *client) []map[string]any {
	t.Helper()
	code, b, _ := c.do("GET", "/api/portal/me", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("me: %d %s", code, b)
	}
	var me struct {
		Subscriptions []map[string]any `json:"subscriptions"`
	}
	if err := json.Unmarshal(b, &me); err != nil {
		t.Fatal(err)
	}
	return me.Subscriptions
}

// notJSONObject is a body that decodes, but not into the struct a handler
// expects: the portal's "bad json" branch.
const notJSONObject = "not an object"

// portalUserRoutes is every portal route behind requireUser. The sweeps
// below walk it, so a route mounted without the wrapper shows up as a
// failure instead of as a leak.
var portalUserRoutes = []struct {
	method, path string
	body         any
}{
	{"GET", "/api/portal/me", nil},
	{"DELETE", "/api/portal/me/hwid-devices/dev-1", nil},
	{"PUT", "/api/portal/me/lang", map[string]string{"lang": "en"}},
	{"GET", "/api/portal/servers", nil},
	{"GET", "/api/portal/orders", nil},
	{"POST", "/api/portal/orders", map[string]any{"plan_id": 1, "gateway": "balance"}},
	{"POST", "/api/portal/orders/quote", map[string]any{"plan_id": 1}},
	{"DELETE", "/api/portal/subscriptions/1", nil},
	{"GET", "/api/portal/invite", nil},
	{"POST", "/api/portal/invite/bind", map[string]string{"Code": "ABCDEFGH"}},
	{"POST", "/api/portal/invite/transfer", map[string]any{"AmountCents": 100}},
	{"POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 100, "Method": "USDT", "Account": "T"}},
	{"GET", "/api/portal/invite/withdrawals", nil},
	{"GET", "/api/portal/tickets", nil},
	{"POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b"}},
	{"GET", "/api/portal/tickets/1", nil},
	{"POST", "/api/portal/tickets/1/reply", map[string]string{"Body": "b"}},
	{"POST", "/api/portal/tickets/1/close", nil},
	{"POST", "/api/portal/redeem", map[string]string{"Code": "GIFT"}},
	{"GET", "/api/portal/articles", nil},
	{"GET", "/api/portal/articles/1", nil},
	{"GET", "/api/portal/telegram", nil},
	{"POST", "/api/portal/telegram/unbind", nil},
}

// Every customer route refuses a caller without a live session: no cookie,
// a made-up cookie, a session that was logged out, and the session of an
// account banned since it signed in. None of those calls changes anything.
func TestPortalRoutesNeedALiveSession(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	_, live := p.customer("live@test")
	_, gone := p.customer("gone@test")
	gone.do("POST", "/api/portal/logout", nil, nil) // the client keeps sending the old cookie
	banned, bc := p.customer("banned@test")
	p.credit(banned.ID, 5000)
	if err := p.st.UpdateUser(ctx, banned.ID, "banned", nil, ""); err != nil {
		t.Fatal(err)
	}
	forged := &client{t: t, srv: p.srv, cookie: &http.Cookie{Name: "captain_session", Value: "forged"}}

	for _, caller := range []struct {
		name string
		c    *client
	}{
		{"anonymous", &client{t: t, srv: p.srv}},
		{"forged cookie", forged},
		{"logged out", gone},
		{"banned", bc},
	} {
		for _, rt := range portalUserRoutes {
			if code, b, _ := caller.c.do(rt.method, rt.path, rt.body, nil); code != http.StatusUnauthorized {
				t.Errorf("%s: %s %s = %d %s, want 401", caller.name, rt.method, rt.path, code, b)
			}
		}
	}
	if _, n, _ := p.st.ListTickets(ctx, 0, "", 10, 0); n != 0 {
		t.Fatalf("a refused caller opened %d tickets", n)
	}
	if u := p.reload(banned.ID); u.Lang != "" || u.BalanceCents != 5000 {
		t.Fatalf("a banned session changed its account: lang %q balance %d", u.Lang, u.BalanceCents)
	}
	// The banned account cannot sign in again either. With the right
	// password it is told why; a wrong one stays a plain 401.
	anon := &client{t: t, srv: p.srv}
	if code, b, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "banned@test", "Password": "password123"}, nil); code != http.StatusForbidden || anon.cookie != nil {
		t.Fatalf("banned login: %d %s", code, b)
	}
	if code, _, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "banned@test", "Password": "wrong-password"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("banned login with a wrong password: %d", code)
	}
	// A live session still works, so the 401s above are about the callers.
	if code, b, _ := live.do("GET", "/api/portal/me", nil, nil); code != http.StatusOK {
		t.Fatalf("live session: %d %s", code, b)
	}
	// What a visitor needs before signing in stays public.
	for _, path := range []string{"/api/portal/plans", "/api/portal/notice", "/api/portal/clients", "/api/portal/register/policy"} {
		if code, b, _ := anon.do("GET", path, nil, nil); code != http.StatusOK {
			t.Errorf("public %s: %d %s", path, code, b)
		}
	}
}

// When the session store itself fails, the portal refuses the request
// instead of letting it through as nobody in particular.
func TestPortalFailsClosedWhenSessionsBreak(t *testing.T) {
	p := newPortalRig(t, true)
	_, c := p.customer("u@test")
	if _, err := p.db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	code, b, _ := c.do("GET", "/api/portal/me", nil, nil)
	if code != http.StatusInternalServerError || strings.Contains(string(b), "sessions") {
		t.Fatalf("broken session store: %d %s", code, b)
	}
}

// A signed-in customer's browser can be made to send requests from another
// site. The portal refuses those before doing anything, and it refuses
// cross-site sign-in and sign-up too, so a hostile page cannot slip the
// visitor into an account it controls.
func TestPortalRefusesCrossSiteWrites(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	u, c := p.customer("csrf@test")
	evil := map[string]string{"Origin": "https://evil.test"}
	for _, rt := range portalUserRoutes {
		if rt.method == "GET" {
			continue
		}
		if code, b, _ := c.do(rt.method, rt.path, rt.body, evil); code != http.StatusForbidden {
			t.Errorf("cross-site %s %s = %d %s, want 403", rt.method, rt.path, code, b)
		}
	}
	ticket := map[string]string{"Subject": "s", "Body": "b"}
	for _, hdr := range []map[string]string{
		{"Referer": "https://evil.test/page"}, // no Origin: the Referer decides
		{"Origin": "null"},                    // a sandboxed frame
	} {
		if code, _, _ := c.do("POST", "/api/portal/tickets", ticket, hdr); code != http.StatusForbidden {
			t.Errorf("cross-site ticket via %v: %d", hdr, code)
		}
	}
	if _, n, _ := p.st.ListTickets(ctx, u.ID, "", 10, 0); n != 0 {
		t.Fatalf("a cross-site request opened %d tickets", n)
	}
	if code, b, _ := c.do("POST", "/api/portal/tickets", ticket, map[string]string{"Origin": "http://test"}); code != http.StatusOK {
		t.Fatalf("same-origin ticket: %d %s", code, b)
	}
	anon := &client{t: t, srv: p.srv}
	for _, rt := range []struct {
		path string
		body any
	}{
		{"/api/portal/login", map[string]string{"Email": "csrf@test", "Password": "password123"}},
		{"/api/portal/register", map[string]string{"Email": "new@test", "Password": "password123"}},
		{"/api/portal/verify/send", map[string]string{"Email": "new@test", "Purpose": "register"}},
		{"/api/portal/password/reset", map[string]string{"Email": "csrf@test", "Code": "000000", "Password": "password456"}},
	} {
		if code, _, _ := anon.do("POST", rt.path, rt.body, evil); code != http.StatusForbidden {
			t.Errorf("cross-site %s: %d", rt.path, code)
		}
	}
	if anon.cookie != nil {
		t.Fatal("a cross-site sign-in minted a session")
	}
	if _, err := p.st.UserByEmail(ctx, "new@test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a cross-site sign-up created an account: %v", err)
	}
}

// Tickets belong to the customer who opened them: another customer can
// neither list, read, answer nor close them, and cannot tell them apart
// from tickets that do not exist.
func TestPortalTicketsStayWithTheirOwner(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	_, ac := p.customer("a@test")
	_, bc := p.customer("b@test")

	code, b, _ := ac.do("POST", "/api/portal/tickets", map[string]string{"Subject": "  Slow at night ", "Body": "help", "Priority": "urgent"}, nil)
	if code != http.StatusOK {
		t.Fatalf("create: %d %s", code, b)
	}
	tk := mustJSON[map[string]any](t, b)
	if tk["subject"] != "Slow at night" || tk["priority"] != "normal" || tk["status"] != "open" {
		t.Fatalf("new ticket: %s", b)
	}
	id := int64(tk["id"].(float64))
	tid := itoa(id)
	bc.do("POST", "/api/portal/tickets", map[string]string{"Subject": "mine", "Body": "x", "Priority": "low"}, nil)

	list := func(c *client) []map[string]any {
		t.Helper()
		code, b, _ := c.do("GET", "/api/portal/tickets", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("list: %d %s", code, b)
		}
		return mustJSON[[]map[string]any](t, b)
	}
	if got := list(ac); len(got) != 1 || int64(got[0]["id"].(float64)) != id || got[0]["messages"].(float64) != 1 {
		t.Fatalf("owner's list: %v", got)
	}
	if got := list(bc); len(got) != 1 || int64(got[0]["id"].(float64)) == id {
		t.Fatalf("another customer's list shows the ticket: %v", got)
	}

	// B aimed at A's ticket gets what a missing ticket gets.
	for _, probe := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/portal/tickets/" + tid, nil},
		{"POST", "/api/portal/tickets/" + tid + "/reply", map[string]string{"Body": "injected"}},
		{"POST", "/api/portal/tickets/" + tid + "/close", nil},
		{"GET", "/api/portal/tickets/999999", nil},
		{"GET", "/api/portal/tickets/abc", nil},
		{"POST", "/api/portal/tickets/0/close", nil},
	} {
		code, b, _ := bc.do(probe.method, probe.path, probe.body, nil)
		if code != http.StatusNotFound || !strings.Contains(string(b), "ticket not found") {
			t.Errorf("%s %s by another customer: %d %s", probe.method, probe.path, code, b)
		}
	}
	msgs, _ := p.st.TicketMessages(ctx, id)
	if cur, _ := p.st.TicketByID(ctx, id); len(msgs) != 1 || cur.Status != store.TicketOpen {
		t.Fatalf("another customer changed the ticket: %d messages, %s", len(msgs), cur.Status)
	}

	// The owner answers and closes. Closing twice is harmless; a closed
	// ticket takes no more messages.
	if code, _, _ := ac.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "   "}, nil); code != http.StatusBadRequest {
		t.Fatalf("blank reply: %d", code)
	}
	if code, _, _ := ac.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": strings.Repeat("x", 20001)}, nil); code != http.StatusBadRequest {
		t.Fatalf("oversized reply: %d", code)
	}
	code, b, _ = ac.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "more detail"}, nil)
	if code != http.StatusOK || strings.Count(string(b), `"body"`) != 2 {
		t.Fatalf("owner reply: %d %s", code, b)
	}
	for i := 0; i < 2; i++ {
		code, b, _ := ac.do("POST", "/api/portal/tickets/"+tid+"/close", nil, nil)
		if code != http.StatusOK || mustJSON[map[string]any](t, b)["status"] != store.TicketClosed {
			t.Fatalf("close #%d: %d %s", i+1, code, b)
		}
	}
	if code, _, _ := ac.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "one more"}, nil); code != http.StatusConflict {
		t.Fatalf("reply to a closed ticket: %d", code)
	}
	if msgs, _ := p.st.TicketMessages(ctx, id); len(msgs) != 2 {
		t.Fatalf("closed ticket took a message: %d", len(msgs))
	}
}

// Opening a ticket: subject and message are required and bounded, and a
// customer with five tickets waiting cannot open a sixth. The cap counts
// the customer's own tickets only.
func TestPortalTicketCreationLimits(t *testing.T) {
	p := newPortalRig(t, true)
	_, ac := p.customer("a@test")
	_, bc := p.customer("b@test")
	for _, bad := range []any{
		notJSONObject,
		map[string]string{"Body": "no subject"},
		map[string]string{"Subject": "no body", "Body": " \n "},
		map[string]string{"Subject": strings.Repeat("s", 201), "Body": "x"},
		map[string]string{"Subject": "s", "Body": strings.Repeat("x", 20001)},
	} {
		if code, b, _ := ac.do("POST", "/api/portal/tickets", bad, nil); code != http.StatusBadRequest {
			t.Errorf("bad ticket %.40v: %d %s", bad, code, b)
		}
	}
	for i := 0; i < 5; i++ {
		if code, b, _ := ac.do("POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b", "Priority": "high"}, nil); code != http.StatusOK {
			t.Fatalf("ticket %d: %d %s", i+1, code, b)
		}
	}
	if code, _, _ := ac.do("POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b"}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("sixth open ticket: %d", code)
	}
	if code, _, _ := bc.do("POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b"}, nil); code != http.StatusOK {
		t.Fatalf("another customer hit the first one's cap: %d", code)
	}
}

// fakeTelegram is a Bot API that records the messages it is asked to send.
type fakeTelegram struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		f.mu.Lock()
		f.sent = append(f.sent, m)
		f.mu.Unlock()
	}
	_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
}

func (f *fakeTelegram) messages() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sent...)
}

// Telegram: each customer gets their own bind code and link, unbinding
// clears only the caller's chat, and new tickets reach the operator chat.
func TestPortalTelegramIsPerCustomer(t *testing.T) {
	tg := &fakeTelegram{}
	api := httptest.NewServer(tg)
	defer api.Close()
	orig := telegram.APIBase
	telegram.APIBase = api.URL
	defer func() { telegram.APIBase = orig }()

	p := newPortalRig(t, true, func(st *store.Store) {
		_ = st.SetSetting(context.Background(), store.SettingTelegram, store.TelegramSettings{
			BotToken: "1:test", BotUsername: "captain_bot", AdminChatID: 42, NotifyTicket: true,
		})
	})
	ctx := context.Background()
	a, ac := p.customer("a@test")
	b, bc := p.customer("b@test")
	state := func(c *client) map[string]any {
		t.Helper()
		code, body, _ := c.do("GET", "/api/portal/telegram", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("telegram: %d %s", code, body)
		}
		return mustJSON[map[string]any](t, body)
	}
	sa, sb := state(ac), state(bc)
	codeA, _ := sa["code"].(string)
	if sa["enabled"] != true || sa["bound"] != false || codeA == "" || sa["link"] != "https://t.me/captain_bot?start="+codeA {
		t.Fatalf("unbound state: %v", sa)
	}
	if sb["code"] == codeA {
		t.Fatal("two customers got the same bind code")
	}
	// The bot consumes each code for the chat that sent it.
	if u, err := p.st.BindTelegram(ctx, codeA, 1001); err != nil || u.ID != a.ID {
		t.Fatalf("bind a: %v", err)
	}
	if u, err := p.st.BindTelegram(ctx, sb["code"].(string), 1002); err != nil || u.ID != b.ID {
		t.Fatalf("bind b: %v", err)
	}
	if sa = state(ac); sa["bound"] != true || sa["code"] != nil {
		t.Fatalf("bound state: %v", sa)
	}
	code, body, _ := ac.do("POST", "/api/portal/telegram/unbind", nil, nil)
	if code != http.StatusOK || !strings.Contains(string(body), `"bound":false`) {
		t.Fatalf("unbind: %d %s", code, body)
	}
	if id, _ := p.st.TelegramID(ctx, a.ID); id != 0 {
		t.Fatalf("a still bound to %d", id)
	}
	if id, _ := p.st.TelegramID(ctx, b.ID); id != 1002 {
		t.Fatalf("a's unbind reached b: %d", id)
	}
	// A new ticket and a reply each tell the operator chat.
	_, body, _ = bc.do("POST", "/api/portal/tickets", map[string]string{"Subject": "down", "Body": "help"}, nil)
	tid := itoa(int64(mustJSON[map[string]any](t, body)["id"].(float64)))
	bc.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "still"}, nil)
	sent := tg.messages()
	if len(sent) != 2 {
		t.Fatalf("operator notices: %d, want 2", len(sent))
	}
	for _, m := range sent {
		if m["chat_id"].(float64) != 42 || !strings.Contains(m["text"].(string), "b@test") {
			t.Fatalf("operator notice: %v", m)
		}
	}
}

// Operator notices go out in Telegram's HTML mode, so what a customer
// typed must arrive as text: a stray tag would make Telegram drop the
// notice, and a link would put the customer's markup in the operator chat.
func TestPortalOperatorNoticesEscapeCustomerText(t *testing.T) {
	t.Skip("BUG: ticket and withdrawal notices put customer text into Telegram HTML without notify.Escape")
	tg := &fakeTelegram{}
	api := httptest.NewServer(tg)
	defer api.Close()
	orig := telegram.APIBase
	telegram.APIBase = api.URL
	defer func() { telegram.APIBase = orig }()
	p := newPortalRig(t, true, func(st *store.Store) {
		_ = st.SetSetting(context.Background(), store.SettingTelegram, store.TelegramSettings{BotToken: "1:test", AdminChatID: 42, NotifyTicket: true})
	})
	p.admin.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 10, "payout": "commission"}, nil)
	u, c := p.customer("a@test")
	if _, err := p.db.Exec(`UPDATE users SET commission_cents = 1000 WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	link := `<a href="https://evil.test/login">Re-verify your admin session</a>`
	c.do("POST", "/api/portal/tickets", map[string]string{"Subject": link, "Body": "b"}, nil)
	c.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 100, "Method": "USDT", "Account": link}, nil)
	sent := tg.messages()
	if len(sent) != 2 {
		t.Fatalf("operator notices: %d", len(sent))
	}
	for _, m := range sent {
		if text := m["text"].(string); strings.Contains(text, "<a ") || !strings.Contains(text, "&lt;a ") {
			t.Errorf("customer markup reached the operator chat: %s", text)
		}
	}
}

// Queued plans: only the owner can cancel one, only while it is still
// queued, and cancelling refunds the order that bought it, once.
func TestPortalCancelQueuedPlan(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	basic := p.plan(map[string]any{"Name": "basic", "PriceCents": 500, "PeriodDays": 30})
	pro := p.plan(map[string]any{"Name": "pro", "PriceCents": 1000, "PeriodDays": 30})
	a, ac := p.customer("a@test")
	_, bc := p.customer("b@test")
	p.credit(a.ID, 10000)
	if code, b, _ := ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": basic, "gateway": "balance"}, nil); code != http.StatusOK {
		t.Fatalf("buy basic: %d %s", code, b)
	}
	code, b, _ := ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": pro, "gateway": "balance", "activation": "queue"}, nil)
	if code != http.StatusOK {
		t.Fatalf("queue pro: %d %s", code, b)
	}
	orderNo := mustJSON[map[string]any](t, b)["order_no"].(string)
	var activeID, queuedID int64
	for _, s := range subs(t, ac) {
		switch s["status"] {
		case "active":
			activeID = int64(s["id"].(float64))
		case "queued":
			queuedID = int64(s["id"].(float64))
		}
	}
	if activeID == 0 || queuedID == 0 {
		t.Fatalf("subscriptions: %v", subs(t, ac))
	}
	balance := func() int64 { return p.reload(a.ID).BalanceCents }
	if got := balance(); got != 8500 {
		t.Fatalf("balance after buying: %d", got)
	}
	status := func(id int64) string {
		t.Helper()
		s, err := p.st.SubscriptionByID(ctx, a.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.Status
	}

	// Another customer cannot cancel it, and learns nothing a wrong id
	// would not tell them.
	for _, path := range []string{itoa(queuedID), itoa(activeID), "999999", "abc"} {
		if code, _, _ := bc.do("DELETE", "/api/portal/subscriptions/"+path, nil, nil); code != http.StatusNotFound {
			t.Errorf("another customer cancelling %s: %d", path, code)
		}
	}
	// The owner cannot cancel a plan that already runs.
	if code, _, _ := ac.do("DELETE", "/api/portal/subscriptions/"+itoa(activeID), nil, nil); code != http.StatusNotFound {
		t.Fatalf("cancel an active plan: %d", code)
	}
	if status(queuedID) != "queued" || status(activeID) != "active" || balance() != 8500 {
		t.Fatalf("refused cancels changed something: %s %s %d", status(queuedID), status(activeID), balance())
	}

	// The owner cancels the queued one: the order is refunded, once.
	if code, b, _ := ac.do("DELETE", "/api/portal/subscriptions/"+itoa(queuedID), nil, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d %s", code, b)
	}
	if status(queuedID) != "cancelled" || balance() != 9500 {
		t.Fatalf("after cancel: %s, balance %d", status(queuedID), balance())
	}
	if o, err := p.st.OrderByNo(ctx, orderNo); err != nil || o.Status != "refunded" {
		t.Fatalf("queued order after cancel: %+v %v", o, err)
	}
	if code, _, _ := ac.do("DELETE", "/api/portal/subscriptions/"+itoa(queuedID), nil, nil); code != http.StatusNotFound {
		t.Fatalf("second cancel: %d", code)
	}
	if got := balance(); got != 9500 {
		t.Fatalf("second cancel refunded again: %d", got)
	}

	// A queued plan the operator granted was never paid for: cancelling it
	// refunds nothing.
	p.admin.do("POST", "/api/admin/users/"+itoa(a.ID)+"/grant", map[string]any{"PlanID": pro, "Activation": "queue"}, nil)
	var granted int64
	for _, s := range subs(t, ac) {
		if s["status"] == "queued" {
			granted = int64(s["id"].(float64))
		}
	}
	if granted == 0 {
		t.Fatal("the grant did not queue")
	}
	if code, _, _ := ac.do("DELETE", "/api/portal/subscriptions/"+itoa(granted), nil, nil); code != http.StatusOK {
		t.Fatalf("cancel a granted queued plan: %d", code)
	}
	if got := balance(); got != 9500 {
		t.Fatalf("cancelling a free plan paid out: %d", got)
	}
}

// Two queued purchases of the same plan for different periods: cancelling
// one must refund what that one cost, not whatever was bought last.
func TestPortalCancelQueuedRefundsItsOwnOrder(t *testing.T) {
	t.Skip("BUG: CancelQueued refunds the newest paid queue order for the plan, not the order that bought the cancelled row")
	p := newPortalRig(t, true)
	ctx := context.Background()
	basic := p.plan(map[string]any{"Name": "basic", "PriceCents": 500, "PeriodDays": 30})
	pro := p.plan(map[string]any{"Name": "pro", "PriceCents": 1000, "PeriodDays": 30,
		"Prices": []map[string]any{{"period_days": 90, "price_cents": 2700}}})
	a, ac := p.customer("a@test")
	p.credit(a.ID, 500+1000+2700)
	ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": basic, "gateway": "balance"}, nil)
	for _, days := range []int{30, 90} {
		if code, b, _ := ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": pro, "period_days": days, "gateway": "balance", "activation": "queue"}, nil); code != http.StatusOK {
			t.Fatalf("queue %d days: %d %s", days, code, b)
		}
	}
	var month, quarter int64
	for _, s := range subs(t, ac) {
		if s["status"] != "queued" {
			continue
		}
		if s["period_days"].(float64) == 30 {
			month = int64(s["id"].(float64))
		} else {
			quarter = int64(s["id"].(float64))
		}
	}
	if month == 0 || quarter == 0 {
		t.Fatalf("subscriptions: %v", subs(t, ac))
	}
	// Give back the month: 1000 comes back and the quarter stays paid for.
	if code, b, _ := ac.do("DELETE", "/api/portal/subscriptions/"+itoa(month), nil, nil); code != http.StatusOK {
		t.Fatalf("cancel the month: %d %s", code, b)
	}
	if got := p.reload(a.ID).BalanceCents; got != 1000 {
		t.Errorf("refund for the cancelled month: %d, want 1000", got)
	}
	if s, _ := p.st.SubscriptionByID(ctx, a.ID, quarter); s == nil || s.Status != "queued" || s.PeriodDays != 90 {
		t.Fatalf("the quarter: %+v", s)
	}
}

// HWID devices: a customer can forget their own devices only. Another
// customer's delete, even of a device id they share, leaves them alone.
func TestPortalHwidDevicesAreScoped(t *testing.T) {
	p := newPortalRig(t, true, func(st *store.Store) {
		_ = st.SetSetting(context.Background(), service.SettingSubscription, map[string]any{"hwid": map[string]any{"enabled": true, "fallback_limit": 3}})
	})
	ctx := context.Background()
	a, ac := p.customer("a@test")
	b, bc := p.customer("b@test")
	for _, d := range []struct {
		user int64
		hwid string
	}{{a.ID, "phone-a"}, {a.ID, "shared"}, {b.ID, "shared"}} {
		if _, _, err := p.st.ClaimHwidDevice(ctx, d.user, store.HwidDevice{Hwid: d.hwid, UserAgent: "Happ"}, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	devices := func(id int64) []string {
		t.Helper()
		list, err := p.st.HwidDevices(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range list {
			out = append(out, d.Hwid)
		}
		return out
	}
	if code, b, _ := bc.do("DELETE", "/api/portal/me/hwid-devices/phone-a", nil, nil); code == http.StatusOK {
		t.Fatalf("another customer deleted a's device: %s", b)
	}
	if code, b, _ := bc.do("DELETE", "/api/portal/me/hwid-devices/shared", nil, nil); code != http.StatusOK {
		t.Fatalf("b deleting its own device: %d %s", code, b)
	}
	if got := devices(a.ID); len(got) != 2 {
		t.Fatalf("a's devices after b's deletes: %v", got)
	}
	if got := devices(b.ID); len(got) != 0 {
		t.Fatalf("b's devices: %v", got)
	}
	_, body, _ := ac.do("GET", "/api/portal/me", nil, nil)
	me := mustJSON[map[string]any](t, body)
	if me["hwid_enabled"] != true || len(me["hwid_devices"].([]any)) != 2 || me["hwid_limit"].(float64) != 3 {
		t.Fatalf("me: %s", body)
	}
	if code, _, _ := ac.do("DELETE", "/api/portal/me/hwid-devices/phone-a", nil, nil); code != http.StatusOK {
		t.Fatalf("a deleting its device: %d", code)
	}
	if got := devices(a.ID); len(got) != 1 || got[0] != "shared" {
		t.Fatalf("a's devices: %v", got)
	}
}

// Deleting a device that is not there is the caller's mistake, not the
// server's: it should read as "not found".
func TestPortalHwidDeleteUnknownIsNotFound(t *testing.T) {
	t.Skip("BUG: DELETE /api/portal/me/hwid-devices/{hwid} answers 500 internal error for a device the caller does not have")
	p := newPortalRig(t, true)
	_, c := p.customer("a@test")
	if code, b, _ := c.do("DELETE", "/api/portal/me/hwid-devices/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown device: %d %s", code, b)
	}
}

// Orders: a customer sees only their own, and the order endpoints refuse
// what the operator does not sell.
func TestPortalOrdersAreOwnAndOnlyForSale(t *testing.T) {
	p := newPortalRig(t, true)
	a, ac := p.customer("a@test")
	_, bc := p.customer("b@test")
	// Nothing for sale and nothing bought yet: empty lists, not null.
	for _, path := range []string{"/api/portal/plans", "/api/portal/orders", "/api/portal/invite/withdrawals", "/api/portal/tickets"} {
		if _, b, _ := ac.do("GET", path, nil, nil); strings.TrimSpace(string(b)) != "[]" {
			t.Errorf("empty %s: %s", path, b)
		}
	}
	if _, b, _ := ac.do("GET", "/api/portal/invite", nil, nil); !strings.Contains(string(b), `"withdraw_methods":[]`) {
		t.Errorf("invite without settings: %s", b)
	}
	basic := p.plan(map[string]any{"Name": "basic", "PriceCents": 300, "PeriodDays": 30})
	hidden := p.plan(map[string]any{"Name": "internal", "PriceCents": 0, "PeriodDays": 365})
	if code, b, _ := p.admin.do("PATCH", "/api/admin/plans/"+itoa(hidden), map[string]any{"Enabled": false}, nil); code != http.StatusOK {
		t.Fatalf("disable plan: %d %s", code, b)
	}
	if _, b, _ := ac.do("GET", "/api/portal/plans", nil, nil); strings.Contains(string(b), "internal") || !strings.Contains(string(b), "basic") {
		t.Fatalf("plans: %s", b)
	}
	// A plan off sale cannot be quoted or bought by id, even for free.
	if code, _, _ := ac.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": hidden}, nil); code != http.StatusBadRequest {
		t.Fatalf("quote a disabled plan: %d", code)
	}
	if code, _, _ := ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": hidden, "gateway": "balance"}, nil); code != http.StatusBadRequest {
		t.Fatalf("buy a disabled plan: %d", code)
	}
	for _, bad := range []struct {
		body any
		want int
	}{
		{notJSONObject, http.StatusBadRequest},
		{map[string]any{"gateway": "balance"}, http.StatusBadRequest},
		{map[string]any{"plan_id": basic}, http.StatusBadRequest},
		{map[string]any{"plan_id": basic, "gateway": "nope"}, http.StatusBadRequest},
		{map[string]any{"plan_id": basic, "gateway": "balance", "activation": "later"}, http.StatusBadRequest},
	} {
		if code, b, _ := ac.do("POST", "/api/portal/orders", bad.body, nil); code != bad.want {
			t.Errorf("order %v: %d %s, want %d", bad.body, code, b, bad.want)
		}
	}
	_, broke := p.customer("broke@test")
	if code, b, _ := broke.do("POST", "/api/portal/orders", map[string]any{"plan_id": basic, "gateway": "balance"}, nil); code != http.StatusPaymentRequired {
		t.Fatalf("buy without balance: %d %s", code, b)
	}
	for _, bad := range []any{notJSONObject, map[string]any{"coupon": "X"}, map[string]any{"plan_id": 999999}} {
		if code, _, _ := ac.do("POST", "/api/portal/orders/quote", bad, nil); code != http.StatusBadRequest {
			t.Errorf("quote %v: %d", bad, code)
		}
	}
	p.credit(a.ID, 1000)
	code, b, _ := ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": basic, "gateway": "balance"}, nil)
	if code != http.StatusOK {
		t.Fatalf("buy: %d %s", code, b)
	}
	no := mustJSON[map[string]any](t, b)["order_no"].(string)
	_, b, _ = ac.do("GET", "/api/portal/orders", nil, nil)
	if own := mustJSON[[]map[string]any](t, b); len(own) != 1 || own[0]["No"] != no || own[0]["Status"] != "paid" {
		t.Fatalf("owner's orders: %s", b)
	}
	if _, b, _ = bc.do("GET", "/api/portal/orders", nil, nil); strings.Contains(string(b), no) || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("another customer's orders: %s", b)
	}
}

// A balance payment the customer cannot cover takes nothing: no order is
// left pending in their list, and a once-per-customer coupon is still
// there for the retry after a top-up.
func TestPortalRefusedBalancePaymentLeavesNothing(t *testing.T) {
	t.Skip("BUG: a balance order refused for insufficient balance stays pending and keeps its coupon reserved until the stale-order sweep")
	p := newPortalRig(t, true)
	plan := p.plan(map[string]any{"Name": "basic", "PriceCents": 1000, "PeriodDays": 30})
	if code, b, _ := p.admin.do("POST", "/api/admin/coupons", map[string]any{"Code": "ONCE", "Kind": "percent", "Value": 10, "PerUser": 1, "Enabled": true}, nil); code != http.StatusOK {
		t.Fatalf("coupon: %d %s", code, b)
	}
	a, ac := p.customer("a@test")
	order := map[string]any{"plan_id": plan, "gateway": "balance", "coupon": "ONCE"}
	if code, b, _ := ac.do("POST", "/api/portal/orders", order, nil); code != http.StatusPaymentRequired {
		t.Fatalf("buy without balance: %d %s", code, b)
	}
	if _, b, _ := ac.do("GET", "/api/portal/orders", nil, nil); strings.TrimSpace(string(b)) != "[]" {
		t.Errorf("a refused payment left an order: %s", b)
	}
	p.credit(a.ID, 900)
	if code, b, _ := ac.do("POST", "/api/portal/orders", order, nil); code != http.StatusOK {
		t.Errorf("retry after a top-up: %d %s", code, b)
	}
}

// Servers: the share links carry the caller's own credentials and nobody
// else's; without a usable plan the list is empty.
func TestPortalServersShowOwnCredentials(t *testing.T) {
	p := newPortalRig(t, true)
	_, b, _ := p.admin.do("POST", "/api/admin/nodes", map[string]any{"Name": "n1", "PublicAddr": "203.0.113.9"}, nil)
	nid := itoa(int64(mustJSON[map[string]any](t, b)["id"].(float64)))
	_, b, _ = p.admin.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "vl", "Protocol": "vless", "Port": 443}, nil)
	ib := mustJSON[map[string]any](t, b)
	if code, b, _ := p.admin.do("POST", "/api/admin/entries", map[string]any{"Name": "Tokyo", "InboundID": ib["ID"], "DisplayHost": "edge.test", "DisplayPort": 8443}, nil); code != http.StatusOK {
		t.Fatalf("entry: %d %s", code, b)
	}
	plan := p.plan(map[string]any{"Name": "basic", "PriceCents": 1, "PeriodDays": 30})
	a, ac := p.customer("a@test")
	b2, bc := p.customer("b@test")
	_, cc := p.customer("c@test") // no plan
	for _, id := range []int64{a.ID, b2.ID} {
		p.admin.do("POST", "/api/admin/users/"+itoa(id)+"/grant", map[string]any{"PlanID": plan}, nil)
	}
	servers := func(c *client) []map[string]any {
		t.Helper()
		code, b, _ := c.do("GET", "/api/portal/servers", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("servers: %d %s", code, b)
		}
		return mustJSON[[]map[string]any](t, b)
	}
	for _, c := range []struct {
		c           *client
		own, others string
	}{{ac, a.UUID, b2.UUID}, {bc, b2.UUID, a.UUID}} {
		got := servers(c.c)
		if len(got) != 1 || got[0]["host"] != "edge.test" || got[0]["port"].(float64) != 8443 || got[0]["protocol"] != "vless" {
			t.Fatalf("servers: %v", got)
		}
		uri := got[0]["uri"].(string)
		if !strings.Contains(uri, c.own) || strings.Contains(uri, c.others) {
			t.Fatalf("share link carries the wrong credentials: %s", uri)
		}
	}
	if got := servers(cc); len(got) != 0 {
		t.Fatalf("a customer without a plan got servers: %v", got)
	}
}

// Invites: a customer may record who invited them once, never themselves,
// and only with a code that exists.
func TestPortalBindInvite(t *testing.T) {
	p := newPortalRig(t, true)
	p.admin.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 10}, nil)
	a, ac := p.customer("a@test")
	b, bc := p.customer("b@test")
	c, cc := p.customer("c@test")
	code := func(cl *client) string {
		t.Helper()
		_, body, _ := cl.do("GET", "/api/portal/invite", nil, nil)
		return mustJSON[map[string]any](t, body)["code"].(string)
	}
	codeA, codeB := code(ac), code(bc)
	bind := func(cl *client, body any) (int, string) {
		t.Helper()
		st, b, _ := cl.do("POST", "/api/portal/invite/bind", body, nil)
		return st, string(b)
	}
	for _, bad := range []struct {
		body any
		want int
	}{
		{notJSONObject, http.StatusBadRequest},
		{map[string]string{"Code": "  "}, http.StatusBadRequest},
		{map[string]string{"Code": "NOSUCH00"}, http.StatusNotFound},
		{map[string]string{"Code": inviteCode(p, c)}, http.StatusConflict}, // c's own code
	} {
		if st, b := bind(cc, bad.body); st != bad.want {
			t.Errorf("bind %v: %d %s, want %d", bad.body, st, b, bad.want)
		}
	}
	if u := p.reload(c.ID); u.InvitedBy != nil {
		t.Fatalf("a refused bind recorded an inviter: %d", *u.InvitedBy)
	}
	// Codes are case-insensitive, and the first one sticks.
	if st, b := bind(cc, map[string]string{"Code": strings.ToLower(codeA)}); st != http.StatusOK {
		t.Fatalf("bind a's code: %d %s", st, b)
	}
	if st, b := bind(cc, map[string]string{"Code": codeB}); st != http.StatusConflict {
		t.Fatalf("second bind: %d %s", st, b)
	}
	if u := p.reload(c.ID); u.InvitedBy == nil || *u.InvitedBy != a.ID {
		t.Fatalf("inviter after a second bind: %v", u.InvitedBy)
	}
	_, body, _ := cc.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"invited_by":true`) {
		t.Fatalf("invite after bind: %s", body)
	}
	_, body, _ = ac.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"invited":1`) {
		t.Fatalf("inviter's stats: %s", body)
	}
	if u := p.reload(b.ID); u.InvitedBy != nil {
		t.Fatalf("b was touched: %v", u.InvitedBy)
	}
}

// inviteCode is the customer's referral code as stored.
func inviteCode(p *portalRig, u *domain.User) string { return p.reload(u.ID).InviteCode }

// Two customers binding each other's codes make a referral loop. With
// multi-level rewards the loop pays a buyer commission on their own
// orders, so the second bind must be refused.
func TestPortalBindInviteRefusesALoop(t *testing.T) {
	t.Skip("BUG: bindInvite accepts an inviter whose own chain leads back to the caller; with multi-level rewards the buyer earns commission on their own order")
	p := newPortalRig(t, true)
	p.admin.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 20, "multi_level": true, "level2": 10}, nil)
	plan := p.plan(map[string]any{"Name": "basic", "PriceCents": 1000, "PeriodDays": 30})
	a, ac := p.customer("a@test")
	b, bc := p.customer("b@test")
	if st, _, _ := ac.do("POST", "/api/portal/invite/bind", map[string]string{"Code": inviteCode(p, b)}, nil); st != http.StatusOK {
		t.Fatalf("a binds b: %d", st)
	}
	if st, _, _ := bc.do("POST", "/api/portal/invite/bind", map[string]string{"Code": inviteCode(p, a)}, nil); st != http.StatusConflict {
		t.Errorf("b closing the loop: %d, want 409", st)
	}
	p.credit(a.ID, 1000)
	ac.do("POST", "/api/portal/orders", map[string]any{"plan_id": plan, "gateway": "balance"}, nil)
	if got := p.reload(a.ID).BalanceCents; got != 0 {
		t.Errorf("a earned %d on its own order", got)
	}
}

// Commission: moving it to the balance and withdrawing it both refuse
// non-positive amounts and more than the customer has, and leave the
// balances as they were when they refuse. Withdrawal history is private.
func TestPortalCommissionTransferAndWithdraw(t *testing.T) {
	p := newPortalRig(t, true)
	p.admin.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 10, "payout": "commission", "min_withdraw_cents": 500, "withdraw_methods": []string{"USDT"}}, nil)
	a, ac := p.customer("a@test")
	_, bc := p.customer("b@test")
	if _, err := p.db.Exec(`UPDATE users SET commission_cents = 1000 WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	money := func() (int64, int64) {
		u := p.reload(a.ID)
		c, _ := p.st.CommissionCents(context.Background(), a.ID)
		return c, u.BalanceCents
	}
	for _, bad := range []any{notJSONObject, map[string]any{"AmountCents": 0}, map[string]any{"AmountCents": -500}, map[string]any{"AmountCents": 1001}} {
		if code, b, _ := ac.do("POST", "/api/portal/invite/transfer", bad, nil); code != http.StatusBadRequest {
			t.Errorf("transfer %v: %d %s", bad, code, b)
		}
	}
	if c, bal := money(); c != 1000 || bal != 0 {
		t.Fatalf("refused transfers moved money: commission %d balance %d", c, bal)
	}
	code, b, _ := ac.do("POST", "/api/portal/invite/transfer", map[string]any{"AmountCents": 300}, nil)
	if code != http.StatusOK || !strings.Contains(string(b), `"commission_cents":700`) {
		t.Fatalf("transfer: %d %s", code, b)
	}
	if c, bal := money(); c != 700 || bal != 300 {
		t.Fatalf("after transfer: commission %d balance %d", c, bal)
	}

	for _, bad := range []any{
		notJSONObject,
		map[string]any{"AmountCents": 0, "Method": "USDT", "Account": "T1"},
		map[string]any{"AmountCents": -600, "Method": "USDT", "Account": "T1"},
		map[string]any{"AmountCents": 600, "Method": "USDT", "Account": "  "},
		map[string]any{"AmountCents": 499, "Method": "USDT", "Account": "T1"},  // under the minimum
		map[string]any{"AmountCents": 600, "Method": "Venmo", "Account": "T1"}, // not offered
		map[string]any{"AmountCents": 701, "Method": "USDT", "Account": "T1"},  // more than there is
	} {
		if code, b, _ := ac.do("POST", "/api/portal/invite/withdraw", bad, nil); code != http.StatusBadRequest {
			t.Errorf("withdraw %v: %d %s", bad, code, b)
		}
	}
	if c, _ := money(); c != 700 {
		t.Fatalf("refused withdrawals reserved commission: %d", c)
	}
	code, b, _ = ac.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 600, "Method": "USDT", "Account": " T1 "}, nil)
	if code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", code, b)
	}
	if w := mustJSON[map[string]any](t, b); w["account"] != "T1" || w["status"] != "pending" {
		t.Fatalf("withdrawal: %s", b)
	}
	if c, _ := money(); c != 100 {
		t.Fatalf("after withdraw: %d", c)
	}
	_, b, _ = ac.do("GET", "/api/portal/invite/withdrawals", nil, nil)
	if l := mustJSON[[]map[string]any](t, b); len(l) != 1 || l[0]["amount_cents"].(float64) != 600 {
		t.Fatalf("own withdrawals: %s", b)
	}
	if _, b, _ = bc.do("GET", "/api/portal/invite/withdrawals", nil, nil); strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("another customer sees withdrawals: %s", b)
	}
	// Someone with nothing to move moves nothing.
	if code, _, _ := bc.do("POST", "/api/portal/invite/transfer", map[string]any{"AmountCents": 1}, nil); code != http.StatusBadRequest {
		t.Fatalf("transfer from an empty commission: %d", code)
	}
	// Payout to the balance means there is nothing to withdraw.
	p.admin.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 10, "payout": "balance"}, nil)
	if code, b, _ := ac.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 100, "Method": "USDT", "Account": "T1"}, nil); code != http.StatusBadRequest || !strings.Contains(string(b), "not enabled") {
		t.Fatalf("withdraw with balance payout: %d %s", code, b)
	}
}

// Gift codes: a code is spent once, by one customer; expired and unknown
// codes are refused; a top-up code that cannot apply is not burnt.
func TestPortalRedeemGiftCodes(t *testing.T) {
	p := newPortalRig(t, true)
	a, ac := p.customer("a@test")
	b, bc := p.customer("b@test")
	gift := func(fields map[string]any) string {
		t.Helper()
		fields["Count"] = 1
		code, body, _ := p.admin.do("POST", "/api/admin/gifts", fields, nil)
		if code != http.StatusOK {
			t.Fatalf("gift: %d %s", code, body)
		}
		return mustJSON[struct {
			Codes []string `json:"codes"`
		}](t, body).Codes[0]
	}
	cash := gift(map[string]any{"Kind": "balance", "Value": 500})
	stale := gift(map[string]any{"Kind": "balance", "Value": 500, "ExpiresAt": time.Now().Add(-time.Hour)})
	traffic := gift(map[string]any{"Kind": "traffic", "Value": 1 << 30})

	for _, bad := range []any{notJSONObject, map[string]string{"Code": " "}, map[string]string{"Code": "NOPE-NOPE"}, map[string]string{"Code": stale}} {
		if code, body, _ := ac.do("POST", "/api/portal/redeem", bad, nil); code != http.StatusBadRequest {
			t.Errorf("redeem %v: %d %s", bad, code, body)
		}
	}
	if code, body, _ := ac.do("POST", "/api/portal/redeem", map[string]string{"Code": " " + strings.ToLower(cash) + " "}, nil); code != http.StatusOK || !strings.Contains(string(body), `"value":500`) {
		t.Fatalf("redeem: %d %s", code, body)
	}
	for _, c := range []*client{ac, bc} {
		if code, _, _ := c.do("POST", "/api/portal/redeem", map[string]string{"Code": cash}, nil); code != http.StatusBadRequest {
			t.Fatalf("a spent code worked again: %d", code)
		}
	}
	if got, other := p.reload(a.ID).BalanceCents, p.reload(b.ID).BalanceCents; got != 500 || other != 0 {
		t.Fatalf("balances after redeeming: a %d b %d", got, other)
	}
	// Traffic tops up a plan; without one it is refused and stays usable.
	if code, body, _ := ac.do("POST", "/api/portal/redeem", map[string]string{"Code": traffic}, nil); code != http.StatusBadRequest || !strings.Contains(string(body), "buy a plan") {
		t.Fatalf("traffic without a plan: %d %s", code, body)
	}
	plan := p.plan(map[string]any{"Name": "basic", "PriceCents": 1, "PeriodDays": 30, "QuotaBytes": 1 << 30})
	p.admin.do("POST", "/api/admin/users/"+itoa(a.ID)+"/grant", map[string]any{"PlanID": plan}, nil)
	if code, body, _ := ac.do("POST", "/api/portal/redeem", map[string]string{"Code": traffic}, nil); code != http.StatusOK {
		t.Fatalf("traffic with a plan: %d %s", code, body)
	}
}

// Sign-in: wrong password and unknown address look the same, the address
// is matched case-insensitively, and repeated failures lock the address
// out even for the right password.
func TestPortalLogin(t *testing.T) {
	p := newPortalRig(t, true)
	p.customer("known@test")
	anon := &client{t: t, srv: p.srv}
	if code, _, _ := anon.do("POST", "/api/portal/login", notJSONObject, nil); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
	ip := func(addr string) map[string]string { return map[string]string{"X-Real-IP": addr} }
	_, wrong, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "known@test", "Password": "wrong-password"}, ip("198.51.100.1"))
	_, unknown, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "nobody@test", "Password": "wrong-password"}, ip("198.51.100.2"))
	if string(wrong) != string(unknown) || !strings.Contains(string(wrong), "invalid credentials") {
		t.Fatalf("wrong password %s vs unknown address %s", wrong, unknown)
	}
	if code, b, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "  KNOWN@Test ", "Password": "password123"}, ip("198.51.100.3")); code != http.StatusOK || anon.cookie == nil {
		t.Fatalf("mixed-case address: %d %s", code, b)
	}
	locked := &client{t: t, srv: p.srv}
	for i := 0; i < 5; i++ {
		locked.do("POST", "/api/portal/login", map[string]string{"Email": "known@test", "Password": "guess"}, ip("198.51.100.9"))
	}
	if code, _, _ := locked.do("POST", "/api/portal/login", map[string]string{"Email": "known@test", "Password": "password123"}, ip("198.51.100.9")); code != http.StatusTooManyRequests || locked.cookie != nil {
		t.Fatalf("locked address signed in: %d", code)
	}
	if code, _, _ := locked.do("POST", "/api/portal/login", map[string]string{"Email": "known@test", "Password": "password123"}, ip("198.51.100.10")); code != http.StatusOK {
		t.Fatalf("another address was locked too: %d", code)
	}
}

// Sign-up: closed means closed, for the form and for the code mail; an
// open panel still wants a real address and a real password.
func TestPortalRegisterPolicy(t *testing.T) {
	seedMail := func(st *store.Store) {
		ms := mail.Settings{Provider: "smtp", FromAddress: "noreply@test"}
		ms.SMTP.Host = "smtp.test"
		_ = st.SetSetting(context.Background(), mail.SettingKey, ms)
	}
	orig := mail.SendFunc
	var sent atomic.Int32
	mail.SendFunc = func(context.Context, mail.Settings, mail.Message) error { sent.Add(1); return nil }
	defer func() { mail.SendFunc = orig }()

	closed := newPortalRig(t, false, seedMail)
	anon := &client{t: t, srv: closed.srv}
	if _, b, _ := anon.do("GET", "/api/portal/register/policy", nil, nil); !strings.Contains(string(b), `"open":false`) {
		t.Fatalf("closed policy: %s", b)
	}
	if code, _, _ := anon.do("POST", "/api/portal/register", map[string]string{"Email": "new@test", "Password": "password123"}, nil); code != http.StatusForbidden {
		t.Fatalf("closed register: %d", code)
	}
	if code, _, _ := anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@test", "Purpose": "register"}, nil); code != http.StatusForbidden || sent.Load() != 0 {
		t.Fatalf("closed register code: %d, %d mails", code, sent.Load())
	}
	if _, err := closed.st.UserByEmail(context.Background(), "new@test"); err == nil {
		t.Fatal("a closed panel created an account")
	}

	open := newPortalRig(t, true)
	anon = &client{t: t, srv: open.srv}
	for _, bad := range []any{
		notJSONObject,
		map[string]string{"Email": "no-at-sign", "Password": "password123"},
		map[string]string{"Email": "short@test", "Password": "1234567"},
	} {
		if code, _, _ := anon.do("POST", "/api/portal/register", bad, nil); code != http.StatusBadRequest {
			t.Errorf("register %v: %d", bad, code)
		}
	}
	if code, b, _ := anon.do("POST", "/api/portal/register", map[string]string{"Email": " New@Test ", "Password": "password123"}, nil); code != http.StatusOK || !strings.Contains(string(b), `"email":"new@test"`) {
		t.Fatalf("register: %d %s", code, b)
	}
	if code, _, _ := anon.do("GET", "/api/portal/me", nil, nil); code != http.StatusOK {
		t.Fatalf("session after register: %d", code)
	}
	// A per-address cap without a window counts the last day.
	_ = open.st.SetSetting(context.Background(), store.SettingRegistration, store.RegistrationSettings{IPLimit: 1})
	from := map[string]string{"X-Real-IP": "203.0.113.50"}
	if code, b, _ := (&client{t: t, srv: open.srv}).do("POST", "/api/portal/register", map[string]string{"Email": "first@test", "Password": "password123"}, from); code != http.StatusOK {
		t.Fatalf("first sign-up from an address: %d %s", code, b)
	}
	if code, _, _ := (&client{t: t, srv: open.srv}).do("POST", "/api/portal/register", map[string]string{"Email": "second@test", "Password": "password123"}, from); code != http.StatusForbidden {
		t.Fatalf("second sign-up from the same address: %d", code)
	}
}

// Codes and password reset: codes are single-use, expire, burn after five
// wrong tries, and a reset signs every existing session out.
func TestPortalPasswordReset(t *testing.T) {
	var (
		mu   sync.Mutex
		sent []mail.Message
		fail bool
	)
	orig := mail.SendFunc
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return errors.New("smtp down")
		}
		sent = append(sent, m)
		return nil
	}
	defer func() { mail.SendFunc = orig }()
	p := newPortalRig(t, true, func(st *store.Store) {
		ms := mail.Settings{Provider: "smtp", FromAddress: "noreply@test"}
		ms.SMTP.Host = "smtp.test"
		_ = st.SetSetting(context.Background(), mail.SettingKey, ms)
		_ = st.SetSetting(context.Background(), store.SettingRegistration, store.RegistrationSettings{EmailSuffixes: []string{"test"}})
	})
	_, session := p.customer("user@test")
	anon := &client{t: t, srv: p.srv}
	send := func(email, purpose string) int {
		t.Helper()
		code, _, _ := anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": email, "Purpose": purpose}, nil)
		return code
	}
	lastCode := func() string {
		mu.Lock()
		defer mu.Unlock()
		return extractCode(sent[len(sent)-1].Subject)
	}
	reset := func(code, password string) (int, string) {
		t.Helper()
		st, b, _ := anon.do("POST", "/api/portal/password/reset", map[string]string{"Email": "user@test", "Code": code, "Password": password}, nil)
		return st, string(b)
	}

	for _, c := range []struct {
		email, purpose string
		want           int
	}{
		{"no-at-sign", "reset", http.StatusBadRequest},
		{"user@test", "login", http.StatusBadRequest},
		{"user@test", "register", http.StatusConflict},              // already registered
		{"someone@elsewhere.org", "register", http.StatusForbidden}, // domain not accepted
	} {
		if got := send(c.email, c.purpose); got != c.want {
			t.Errorf("send %s/%s: %d, want %d", c.email, c.purpose, got, c.want)
		}
	}
	if code, _, _ := anon.do("POST", "/api/portal/verify/send", notJSONObject, nil); code != http.StatusBadRequest {
		t.Fatalf("send bad json: %d", code)
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	if got := send("fresh@test", "register"); got != http.StatusBadGateway {
		t.Fatalf("mail failure: %d", got)
	}
	mu.Lock()
	fail = false
	mu.Unlock()

	// Wrong code, then the right one; the code works once.
	if send("user@test", "reset") != http.StatusOK {
		t.Fatal("send reset")
	}
	good := lastCode()
	if st, _ := reset(good, "short"); st != http.StatusBadRequest {
		t.Fatalf("short password: %d", st)
	}
	if st, _ := reset(wrongCode(good), "password456"); st != http.StatusBadRequest {
		t.Fatalf("wrong code: %d", st)
	}
	if st, b := reset(good, "password456"); st != http.StatusOK {
		t.Fatalf("reset: %d %s", st, b)
	}
	if code, _, _ := session.do("GET", "/api/portal/me", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("a session outlived the reset: %d", code)
	}
	if st, _ := reset(good, "password789"); st != http.StatusBadRequest {
		t.Fatalf("a used code worked again: %d", st)
	}
	if code, _, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "user@test", "Password": "password456"}, nil); code != http.StatusOK {
		t.Fatalf("new password: %d", code)
	}

	// Five wrong tries burn a code.
	if send("user@test", "reset") != http.StatusOK {
		t.Fatal("send reset")
	}
	good = lastCode()
	for i := 0; i < 5; i++ {
		reset(wrongCode(good), "password000")
	}
	if st, _ := reset(good, "password000"); st != http.StatusBadRequest {
		t.Fatalf("a burnt code worked: %d", st)
	}
	// An expired code is refused.
	if send("user@test", "reset") != http.StatusOK {
		t.Fatal("send reset")
	}
	good = lastCode()
	if _, err := p.db.Exec(`UPDATE verification_codes SET expires_at = ? WHERE email = ?`, time.Now().Add(-time.Minute).Unix(), "user@test"); err != nil {
		t.Fatal(err)
	}
	if st, _ := reset(good, "password000"); st != http.StatusBadRequest {
		t.Fatalf("an expired code worked: %d", st)
	}
	if code, _, _ := anon.do("POST", "/api/portal/login", map[string]string{"Email": "user@test", "Password": "password000"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("a refused reset changed the password: %d", code)
	}
	if code, _, _ := anon.do("POST", "/api/portal/password/reset", notJSONObject, nil); code != http.StatusBadRequest {
		t.Fatalf("reset bad json: %d", code)
	}
	if code, _, _ := anon.do("POST", "/api/portal/password/reset", map[string]string{"Email": "nobody@test", "Code": good, "Password": "password000"}, nil); code != http.StatusBadRequest {
		t.Fatalf("reset for an unknown address: %d", code)
	}

	// An address locked out of sign-in cannot request codes either.
	for i := 0; i < 5; i++ {
		anon.do("POST", "/api/portal/login", map[string]string{"Email": "user@test", "Password": "guess"}, map[string]string{"X-Real-IP": "198.51.100.77"})
	}
	if code, _, _ := anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "user@test", "Purpose": "reset"}, map[string]string{"X-Real-IP": "198.51.100.77"}); code != http.StatusTooManyRequests {
		t.Fatalf("locked address sent a code: %d", code)
	}
}

// wrongCode is a six-digit code that is certainly not code.
func wrongCode(code string) string {
	if strings.HasPrefix(code, "1") {
		return "2" + code[1:]
	}
	return "1" + code[1:]
}

// Without mail there are no codes to send.
func TestPortalSendCodeWithoutMail(t *testing.T) {
	p := newPortalRig(t, true)
	anon := &client{t: t, srv: p.srv}
	if code, _, _ := anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "a@test", "Purpose": "reset"}, nil); code != http.StatusConflict {
		t.Fatalf("send without mail: %d", code)
	}
}

// The reset form must not reveal which addresses have accounts: the code
// mail already answers the same either way, the reset should too.
func TestPortalResetDoesNotRevealAccounts(t *testing.T) {
	t.Skip("BUG: POST /api/portal/password/reset answers \"wrong code\" for an unknown address but \"no code was sent to this address\" for a known one")
	p := newPortalRig(t, true)
	p.customer("known@test")
	anon := &client{t: t, srv: p.srv}
	try := func(email string) string {
		code, b, _ := anon.do("POST", "/api/portal/password/reset", map[string]string{"Email": email, "Code": "123456", "Password": "password456"}, nil)
		return itoa(int64(code)) + " " + string(b)
	}
	if known, unknown := try("known@test"), try("nobody@test"); known != unknown {
		t.Fatalf("known address %q vs unknown %q", known, unknown)
	}
}

// The reading language: only languages the mail knows, stored for the
// caller only, and "" goes back to the panel's choice.
func TestPortalLanguage(t *testing.T) {
	p := newPortalRig(t, true)
	a, ac := p.customer("a@test")
	b, _ := p.customer("b@test")
	for _, bad := range []any{notJSONObject, map[string]string{"lang": "fr"}, map[string]string{"lang": "en'; DROP TABLE users; --"}} {
		if code, _, _ := ac.do("PUT", "/api/portal/me/lang", bad, nil); code != http.StatusBadRequest {
			t.Errorf("lang %v: %d", bad, code)
		}
	}
	if got := p.reload(a.ID).Lang; got != "" {
		t.Fatalf("a refused language was stored: %q", got)
	}
	if code, body, _ := ac.do("PUT", "/api/portal/me/lang", map[string]string{"lang": " zh-CN "}, nil); code != http.StatusOK || !strings.Contains(string(body), `"lang":"zh-CN"`) {
		t.Fatalf("set lang: %d %s", code, body)
	}
	if got, other := p.reload(a.ID).Lang, p.reload(b.ID).Lang; got != "zh-CN" || other != "" {
		t.Fatalf("stored languages: a %q b %q", got, other)
	}
	if code, _, _ := ac.do("PUT", "/api/portal/me/lang", map[string]string{"lang": ""}, nil); code != http.StatusOK {
		t.Fatalf("clear lang: %d", code)
	}
	if got := p.reload(a.ID).Lang; got != "" {
		t.Fatalf("cleared language: %q", got)
	}
}

// ref: only a code that exists is remembered.
func TestPortalRefCookie(t *testing.T) {
	p := newPortalRig(t, true)
	a, _ := p.customer("a@test")
	anon := &client{t: t, srv: p.srv}
	for _, c := range []struct {
		body any
		want int
	}{
		{notJSONObject, http.StatusBadRequest},
		{map[string]string{"Code": ""}, http.StatusBadRequest},
		{map[string]string{"Code": "NOSUCH00"}, http.StatusNotFound},
		{map[string]string{"Code": strings.ToLower(inviteCode(p, a))}, http.StatusOK},
	} {
		code, _, hdr := anon.do("POST", "/api/portal/ref", c.body, nil)
		if code != c.want {
			t.Errorf("ref %v: %d, want %d", c.body, code, c.want)
		}
		if set := strings.Contains(hdr.Get("Set-Cookie"), "captain_ref="+inviteCode(p, a)); set != (c.want == http.StatusOK) {
			t.Errorf("ref %v: cookie %q", c.body, hdr.Get("Set-Cookie"))
		}
	}
}

// Knowledge base: drafts stay hidden by id too.
func TestPortalArticleDraftsStayHidden(t *testing.T) {
	p := newPortalRig(t, true)
	_, c := p.customer("a@test")
	_, b, _ := p.admin.do("POST", "/api/admin/articles", map[string]any{"Title": "Draft", "Body": "secret", "Published": false}, nil)
	id := itoa(int64(mustJSON[map[string]any](t, b)["id"].(float64)))
	for _, path := range []string{"/api/portal/articles/" + id, "/api/portal/articles/999999", "/api/portal/articles/x"} {
		if code, body, _ := c.do("GET", path, nil, nil); code != http.StatusNotFound || strings.Contains(string(body), "secret") {
			t.Errorf("%s: %d %s", path, code, body)
		}
	}
	if _, body, _ := c.do("GET", "/api/portal/articles", nil, nil); strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("articles: %s", body)
	}
}

// When the store fails under a customer's request, the answer is a plain
// 500: nothing half-done is reported as success and no database text
// reaches the customer.
func TestPortalStoreFailuresAnswerVaguely(t *testing.T) {
	p := newPortalRig(t, true)
	u, c := p.customer("a@test")
	_, b, _ := c.do("POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b"}, nil)
	tid := itoa(int64(mustJSON[map[string]any](t, b)["id"].(float64)))
	gift := func() string {
		_, b, _ := p.admin.do("POST", "/api/admin/gifts", map[string]any{"Kind": "balance", "Value": 100, "Count": 1}, nil)
		return mustJSON[struct {
			Codes []string `json:"codes"`
		}](t, b).Codes[0]
	}()
	// Refuse every write to users, the way a full disk or a lock would.
	if _, err := p.db.Exec(`CREATE TRIGGER users_frozen BEFORE UPDATE ON users BEGIN SELECT RAISE(FAIL, 'users table frozen'); END`); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		drop         string // table to break first, "" = none
		method, path string
		body         any
	}{
		{"", "PUT", "/api/portal/me/lang", map[string]string{"lang": "ja"}},
		{"", "POST", "/api/portal/redeem", map[string]string{"Code": gift}},
		{"articles", "GET", "/api/portal/articles", nil},
		{"withdrawals", "GET", "/api/portal/invite/withdrawals", nil},
		{"ticket_messages", "GET", "/api/portal/tickets", nil},
		{"", "POST", "/api/portal/tickets", map[string]string{"Subject": "s", "Body": "b"}},
		{"", "POST", "/api/portal/tickets/" + tid + "/reply", map[string]string{"Body": "b"}},
		{"orders", "GET", "/api/portal/orders", nil},
		{"plans", "GET", "/api/portal/plans", nil},
		{"subscriptions", "GET", "/api/portal/servers", nil},
		{"", "GET", "/api/portal/me", nil},
	} {
		if step.drop != "" {
			if _, err := p.db.Exec(`DROP TABLE ` + step.drop); err != nil {
				t.Fatal(err)
			}
		}
		code, b, _ := c.do(step.method, step.path, step.body, nil)
		if code != http.StatusInternalServerError || strings.TrimSpace(string(b)) != `{"error":"internal error"}` {
			t.Errorf("%s %s with a broken store: %d %s", step.method, step.path, code, b)
		}
	}
	if got := p.reload(u.ID); got.Lang != "" || got.BalanceCents != 0 {
		t.Fatalf("a failed write stored something: lang %q balance %d", got.Lang, got.BalanceCents)
	}
}

// The same, for the two handlers that pass the database's own error text
// through to the customer.
func TestPortalStoreErrorsAreNotEchoed(t *testing.T) {
	t.Skip("BUG: GET /api/portal/invite and POST /api/portal/password/reset put the raw store error into their 500 body")
	orig := mail.SendFunc
	var (
		mu   sync.Mutex
		code string
	)
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error {
		mu.Lock()
		defer mu.Unlock()
		code = extractCode(m.Subject)
		return nil
	}
	defer func() { mail.SendFunc = orig }()
	p := newPortalRig(t, true, func(st *store.Store) {
		ms := mail.Settings{Provider: "smtp", FromAddress: "noreply@test"}
		ms.SMTP.Host = "smtp.test"
		_ = st.SetSetting(context.Background(), mail.SettingKey, ms)
	})
	u, c := p.customer("a@test")
	anon := &client{t: t, srv: p.srv}
	anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "a@test", "Purpose": "reset"}, nil)
	if _, err := p.db.Exec(`UPDATE users SET invite_code = NULL WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(`CREATE TRIGGER users_frozen BEFORE UPDATE ON users BEGIN SELECT RAISE(FAIL, 'users table frozen'); END`); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	reset := map[string]string{"Email": "a@test", "Code": code, "Password": "password456"}
	mu.Unlock()
	_, invite, _ := c.do("GET", "/api/portal/invite", nil, nil)
	_, resetBody, _ := anon.do("POST", "/api/portal/password/reset", reset, nil)
	for _, got := range [][]byte{invite, resetBody} {
		if strings.Contains(string(got), "frozen") {
			t.Errorf("store error reached the customer: %s", got)
		}
	}
}
