package http

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

const resetPath = "/api/admin/system/reset"

func resetInput() map[string]any {
	return map[string]any{"password": "password123", "confirmation": "RESET", "nodes_acknowledged": true}
}

func TestSiteResetAuthorizationAndFreshStart(t *testing.T) {
	r := newMigratedRig(t)
	ctx := context.Background()
	id, _ := r.user("customer@example.com")
	u, _ := r.st.UserByID(ctx, id)
	portal := &client{t: t, srv: r.srv}
	portal.do("POST", "/api/portal/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
	op, _ := admin.NewUser("operator@example.com", "password123", "operator")
	if err := r.st.CreateStaff(ctx, op); err != nil {
		t.Fatal(err)
	}
	operator := &client{t: t, srv: r.srv}
	operator.do("POST", "/api/admin/login", map[string]string{"Email": op.Email, "Password": "password123"}, nil)
	token, _, err := r.st.CreateAPIToken(ctx, 1, "test", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*client{portal, operator, {t: t, srv: r.srv}, {t: t, srv: r.srv, token: token}} {
		for _, method := range []string{"GET", "POST"} {
			if code, _, _ := c.do(method, resetPath, resetInput(), nil); code != 401 && code != 403 {
				t.Fatalf("unauthorized %s reset: %d", method, code)
			}
		}
	}
	if code, _, _ := r.c.do("POST", resetPath, resetInput(), map[string]string{"Origin": "https://evil.example.com"}); code != 403 {
		t.Fatalf("cross-origin reset: %d", code)
	}
	for _, tc := range []struct {
		field  string
		value  any
		status int
	}{
		{"password", "wrong", 401}, {"confirmation", "reset", 400}, {"nodes_acknowledged", false, 400},
	} {
		in := resetInput()
		in[tc.field] = tc.value
		if code, b, _ := r.c.do("POST", resetPath, in, nil); code != tc.status {
			t.Fatalf("%s: %d %s", tc.field, code, b)
		}
		if _, err := r.st.UserByID(ctx, id); err != nil {
			t.Fatal("rejected reset changed data", err)
		}
	}
	code, body, _ := r.c.do("GET", resetPath, nil, nil)
	preview := mustJSON[map[string]int64](t, body)
	if code != 200 || preview["users"] != 1 || preview["staff"] != 2 || preview["nodes"] != 1 {
		t.Fatalf("preview: %d %s", code, body)
	}
	if code, b, _ := r.c.do("PUT", "/api/admin/admins/1/id", map[string]int64{"id": 9}, nil); code != 200 {
		t.Fatalf("admin ID: %d %s", code, b)
	}
	if err := r.st.SetSetting(ctx, store.SettingNotice, store.NoticeSettings{Enabled: true, Title: "old"}); err != nil {
		t.Fatal(err)
	}
	code, body, headers := r.c.do("POST", resetPath, resetInput(), nil)
	if code != 200 || mustJSON[map[string]any](t, body)["admin_id"] != float64(1) {
		t.Fatalf("reset: %d %s", code, body)
	}
	if len(headers.Values("Set-Cookie")) == 0 {
		t.Fatal("session cookie was not expired")
	}
	for _, c := range []*client{r.c, operator, {t: t, srv: r.srv, token: token}} {
		if code, _, _ := c.do("GET", "/api/admin/me", nil, nil); code < 400 {
			t.Fatal("old admin authorization survived")
		}
	}
	if code, _, _ := portal.do("GET", "/api/portal/me", nil, nil); code < 400 {
		t.Fatal("old customer session survived")
	}
	if code, _, _ := portal.do("GET", "/sub/"+u.SubToken, nil, nil); code < 400 {
		t.Fatal("old subscription survived")
	}
	if code, _, _ := r.agent.do("POST", "/api/agent/report", agentproto.Report{}, nil); code != 401 {
		t.Fatalf("old node token: %d", code)
	}
	if code, b, _ := r.c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 200 {
		t.Fatalf("login after reset: %d %s", code, b)
	}
	_, body, _ = r.c.do("GET", resetPath, nil, nil)
	for key, value := range mustJSON[map[string]int64](t, body) {
		want := int64(0)
		if key == "staff" {
			want = 1
		}
		if value != want {
			t.Errorf("%s: got %d want %d", key, value, want)
		}
	}
	_, body, _ = r.c.do("POST", "/api/admin/users", map[string]string{"Email": "new@example.com", "Password": "password123"}, nil)
	if mustJSON[map[string]any](t, body)["id"] != float64(1) {
		t.Fatalf("first customer: %s", body)
	}
	var settings int
	if err := r.st.DB().QueryRow(`SELECT COUNT(*) FROM settings WHERE key <> 'agent_https'`).Scan(&settings); err != nil || settings != 0 {
		t.Fatal("settings not cleared", settings, err)
	}
	var actor int64
	if err := r.st.DB().QueryRow(`SELECT user_id FROM admin_log WHERE path = ? AND status = 200`, resetPath).Scan(&actor); err != nil || actor != 1 {
		t.Fatal("reset audit actor", actor, err)
	}
}

func TestSiteResetRequiresFreshTOTP(t *testing.T) {
	r := newMigratedRig(t)
	ctx := context.Background()
	secret := auth.NewTOTPSecret()
	if err := r.st.SetTOTP(ctx, 1, secret, true); err != nil {
		t.Fatal(err)
	}
	in := resetInput()
	if code, _, _ := r.c.do("POST", resetPath, in, nil); code != 428 {
		t.Fatalf("missing TOTP: %d", code)
	}
	in["code"] = "invalid"
	if code, _, _ := r.c.do("POST", resetPath, in, nil); code != 401 {
		t.Fatalf("bad TOTP: %d", code)
	}
	// A code accepted for sign-in cannot also authorize destruction.
	code := auth.TOTPCode(secret, time.Now())
	if !auth.VerifyTOTPOnce("staff:"+auth.SHA256Hex(secret), secret, code, time.Now()) {
		t.Fatal("could not consume code")
	}
	in["code"] = code
	if status, _, _ := r.c.do("POST", resetPath, in, nil); status != 401 {
		t.Fatalf("replayed TOTP: %d", status)
	}
	in["code"] = auth.TOTPCode(secret, time.Now().Add(30*time.Second))
	if status, b, _ := r.c.do("POST", resetPath, in, nil); status != 200 {
		t.Fatalf("valid TOTP: %d %s", status, b)
	}
	if got, enabled, err := r.st.TOTP(ctx, 1); err != nil || got != secret || !enabled {
		t.Fatal("authenticator changed", err)
	}
	if status, _, _ := r.c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); status != 428 {
		t.Fatal("reset disabled TOTP", status)
	}
}

func TestSiteResetPasswordAttemptsAreLimited(t *testing.T) {
	r := newMigratedRig(t)
	in := resetInput()
	in["password"] = "wrong"
	for range 5 {
		if status, _, _ := r.c.do("POST", resetPath, in, nil); status != 401 {
			t.Fatal(status)
		}
	}
	if status, _, _ := r.c.do("POST", resetPath, resetInput(), nil); status != http.StatusTooManyRequests {
		t.Fatal("reset bypassed login rate limit", status)
	}
}

func TestSiteResetRevokesWaitingNodeBeforeIDReuse(t *testing.T) {
	r := newMigratedRig(t)
	_, _, headers := r.agent.do("GET", "/api/agent/state", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/agent/state?wait=30s", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.agent.token)
	req.Header.Set("If-None-Match", headers.Get("ETag"))
	result := make(chan int, 1)
	go func() {
		resp, err := r.srv.Client().Do(req)
		if err != nil {
			result <- 0
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		result <- resp.StatusCode
	}()
	// The matching state must stay pending while the old token is valid.
	select {
	case status := <-result:
		t.Fatalf("long poll returned early: %d", status)
	case <-time.After(100 * time.Millisecond):
	}
	if code, b, _ := r.c.do("POST", resetPath, resetInput(), nil); code != 200 {
		t.Fatalf("reset: %d %s", code, b)
	}
	r.c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	code, b, _ := r.c.do("POST", "/api/admin/nodes", map[string]string{"Name": "replacement"}, nil)
	if code != 200 || mustJSON[map[string]any](t, b)["id"] != float64(r.nodeID) {
		t.Fatalf("node ID not reused: %d %s", code, b)
	}
	select {
	case status := <-result:
		if status != 401 {
			t.Fatalf("old long poll survived reset and ID reuse: %d", status)
		}
	case <-ctx.Done():
		t.Fatal("old long poll was not revoked")
	}
}
