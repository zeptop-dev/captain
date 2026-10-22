package http

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestLoginProviderNewAPIFlow(t *testing.T) {
	r := newRigAt(t, "https://captain.example.com")
	ctx := context.Background()
	uid, _ := r.user("alice@example.com")
	alice := &client{t: t, srv: r.srv}
	code, body, _ := alice.do("POST", "/api/portal/login", map[string]string{"Email": "alice@example.com", "Password": "password123"}, nil)
	if code != 200 {
		t.Fatal(string(body))
	}
	bobID, _ := r.user("bob@example.com")
	_ = bobID
	bob := &client{t: t, srv: r.srv}
	bob.do("POST", "/api/portal/login", map[string]string{"Email": "bob@example.com", "Password": "password123"}, nil)
	path := "/api/admin/settings/login-clients"
	c := store.LoginClient{Name: "New API", RedirectURI: "https://newapi.example.com/oauth/captain", Enabled: true}
	code, body, _ = r.c.do("POST", path, c, nil)
	if code != 200 {
		t.Fatal(string(body))
	}
	var created struct {
		Client store.LoginClient `json:"client"`
		Secret string            `json:"client_secret"`
	}
	if e := json.Unmarshal(body, &created); e != nil {
		t.Fatal(e)
	}
	c = created.Client
	if created.Secret == "" {
		t.Fatal("missing secret")
	}
	_, body, _ = r.c.do("GET", path, nil, nil)
	if strings.Contains(string(body), created.Secret) || strings.Contains(string(body), "secret_hash") {
		t.Fatal("secret exposed")
	}
	if code, _, _ := alice.do("GET", path, nil, nil); code != 401 {
		t.Fatalf("customer got admin list: %d", code)
	}
	// Reuse the exact protocol used by New API GenericOAuthProvider: code,
	// redirect_uri and client credentials in form data, then Bearer userinfo.
	query := url.Values{"client_id": {c.ID}, "redirect_uri": {c.RedirectURI}, "response_type": {"code"}, "state": {"browser-bound-state"}, "scope": {"profile email"}}
	request := func(who *client, q url.Values) string {
		t.Helper()
		code, b, _ := who.do("GET", "/api/login-provider/request?"+q.Encode(), nil, nil)
		if code != 200 {
			t.Fatalf("request %d %s", code, b)
		}
		return mustJSON[map[string]string](t, b)["ticket"]
	}
	decide := func(who *client, ticket string, allow bool, origin string, want int) url.Values {
		t.Helper()
		code, b, _ := who.do("POST", "/api/login-provider/decision", map[string]any{"ticket": ticket, "allow": allow}, map[string]string{"Origin": origin})
		if code != want {
			t.Fatalf("decision %d %s", code, b)
		}
		if code != 200 {
			return nil
		}
		dest, e := url.Parse(mustJSON[map[string]string](t, b)["redirect"])
		if e != nil {
			t.Fatal(e)
		}
		if dest.Scheme+"://"+dest.Host+dest.Path != c.RedirectURI {
			t.Fatal("bad callback")
		}
		if dest.Query().Get("state") != query.Get("state") {
			t.Fatal("state lost")
		}
		return dest.Query()
	}
	exchange := func(code, secret, redirect, verifier string, basic bool) (int, map[string]any) {
		t.Helper()
		v := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
		if verifier != "" {
			v.Set("code_verifier", verifier)
		}
		if !basic {
			v.Set("client_id", c.ID)
			v.Set("client_secret", secret)
		}
		req, _ := http.NewRequest("POST", r.srv.URL+"/api/login-provider/token", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if basic {
			req.SetBasicAuth(c.ID, secret)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var out map[string]any
		if e = json.NewDecoder(res.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("token cacheable")
		}
		return res.StatusCode, out
	}
	info := func(token string, want int) map[string]any {
		t.Helper()
		anon := &client{t: t, srv: r.srv}
		code, b, _ := anon.do("GET", "/api/login-provider/userinfo", nil, map[string]string{"Authorization": "Bearer " + token})
		if code != want {
			t.Fatalf("userinfo %d %s", code, b)
		}
		return mustJSON[map[string]any](t, b)
	}
	// Staff and anonymous sessions cannot authorize, even with overlapping IDs.
	for _, who := range []*client{r.c, {t: t, srv: r.srv}} {
		if code, _, _ := who.do("GET", "/api/login-provider/request?"+query.Encode(), nil, nil); code != 401 {
			t.Fatalf("non-customer authorized: %d", code)
		}
	}
	for _, change := range []struct{ k, v string }{{"redirect_uri", c.RedirectURI + "/other"}, {"scope", "openid profile email"}, {"response_type", "token"}, {"state", ""}, {"code_challenge_method", "plain"}} {
		q := cloneLoginQuery(query)
		q.Set(change.k, change.v)
		if code, _, h := alice.do("GET", "/api/login-provider/authorize?"+q.Encode(), nil, nil); code != 400 || h.Get("Location") != "" {
			t.Fatal("bad request redirected")
		}
	}
	ticket := request(alice, query)
	decide(bob, ticket, true, "https://captain.example.com", 400)
	decide(alice, ticket, true, "https://evil.example.com", 403)
	q := decide(alice, ticket, true, "https://captain.example.com", 200)
	decide(alice, ticket, true, "https://captain.example.com", 400)
	if status, _ := exchange(q.Get("code"), "wrong", c.RedirectURI, "", false); status != 401 {
		t.Fatal("bad secret accepted")
	}
	if status, _ := exchange(q.Get("code"), created.Secret, c.RedirectURI+"/other", "", false); status != 400 {
		t.Fatal("wrong callback accepted")
	}
	status, tokens := exchange(q.Get("code"), created.Secret, c.RedirectURI, "", false)
	if status != 200 {
		t.Fatal(tokens)
	}
	token := tokens["access_token"].(string)
	if status, _ := exchange(q.Get("code"), created.Secret, c.RedirectURI, "", false); status != 400 {
		t.Fatal("code replay accepted")
	}
	user := info(token, 200)
	subject := user["sub"].(string)
	if subject == "" || user["email"] != "alice@example.com" || user["email_verified"] != false {
		t.Fatal(user)
	}
	for _, field := range []string{"id", "uuid", "subscription_url", "balance_cents", "role", "password_hash"} {
		if _, ok := user[field]; ok {
			t.Fatalf("leaked %s", field)
		}
	}
	// A provider token is not a portal session or an admin credential.
	anon := &client{t: t, srv: r.srv}
	if code, _, _ := anon.do("GET", "/api/portal/me", nil, map[string]string{"Authorization": "Bearer " + token}); code != 401 {
		t.Fatal("token accessed portal")
	}
	// Subject survives renumbering and email updates.
	if code, b, _ := r.c.do("PUT", "/api/admin/users/"+itoa(uid)+"/id", map[string]int64{"id": 400}, nil); code != 200 {
		t.Fatal(string(b))
	}
	if info(token, 200)["sub"] != subject {
		t.Fatal("subject changed on renumber")
	}
	// Scope limits personal data; Basic authentication and S256 PKCE supported.
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	pkce := cloneLoginQuery(query)
	pkce.Set("scope", "profile")
	pkce.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	pkce.Set("code_challenge_method", "S256")
	q = decide(alice, request(alice, pkce), true, "https://captain.example.com", 200)
	if status, _ := exchange(q.Get("code"), created.Secret, c.RedirectURI, "", true); status != 400 {
		t.Fatal("missing verifier accepted")
	}
	status, tokens = exchange(q.Get("code"), created.Secret, c.RedirectURI, verifier, true)
	if status != 200 {
		t.Fatal(tokens)
	}
	if _, ok := info(tokens["access_token"].(string), 200)["email"]; ok {
		t.Fatal("email scope not enforced")
	}
	denied := decide(alice, request(alice, query), false, "https://captain.example.com", 200)
	if denied.Get("error") != "access_denied" || denied.Get("code") != "" {
		t.Fatal("denial issued code")
	}
	// Changing an application's configuration revokes codes and tokens.
	pending := request(alice, query)
	c.Enabled = false
	if code, b, _ := r.c.do("PUT", path+"/"+c.ID, c, nil); code != 200 {
		t.Fatal(string(b))
	}
	info(token, 401)
	decide(alice, pending, true, "https://captain.example.com", 400)
	c.Enabled = true
	r.c.do("PUT", path+"/"+c.ID, c, nil)
	q = decide(alice, request(alice, query), true, "https://captain.example.com", 200)
	_, body, _ = r.c.do("POST", path+"/"+c.ID+"/rotate", nil, nil)
	newSecret := mustJSON[map[string]string](t, body)["client_secret"]
	if status, _ := exchange(q.Get("code"), created.Secret, c.RedirectURI, "", false); status != 401 {
		t.Fatal("old secret accepted")
	}
	if status, _ := exchange(q.Get("code"), newSecret, c.RedirectURI, "", false); status != 400 {
		t.Fatal("rotation retained code")
	}
	// Logout removes linked Captain access tokens; New API's own session is separate.
	q = decide(alice, request(alice, query), true, "https://captain.example.com", 200)
	status, tokens = exchange(q.Get("code"), newSecret, c.RedirectURI, "", false)
	if status != 200 {
		t.Fatal(tokens)
	}
	alice.do("POST", "/api/portal/logout", nil, nil)
	info(tokens["access_token"].(string), 401)
	if e := r.st.DeleteLoginClient(ctx, c.ID); e != nil {
		t.Fatal(e)
	}
}

func TestLoginProviderConcurrentExchangeAndExpiry(t *testing.T) {
	r := newRigAt(t, "https://captain.example.com")
	ctx := context.Background()
	uid, _ := r.user("race@example.com")
	c, secret, e := r.st.SaveLoginClient(ctx, store.LoginClient{Name: "Example", RedirectURI: "https://app.example.com/callback", Enabled: true}, true)
	if e != nil {
		t.Fatal(e)
	}
	sessions := &sessionAuth{store: r.st}
	session, e := sessions.Create(ctx, uid, false)
	if e != nil {
		t.Fatal(e)
	}
	newCode := func() string {
		t.Helper()
		ticket, e := r.st.NewLoginRequest(ctx, store.LoginRequest{ClientID: c.ID, SessionID: session.ID, RedirectURI: c.RedirectURI, State: "state", Scope: "profile"})
		if e != nil {
			t.Fatal(e)
		}
		_, code, e := r.st.DecideLoginRequest(ctx, ticket, session.ID, true)
		if e != nil {
			t.Fatal(e)
		}
		return code
	}
	code := newCode()
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := r.st.ExchangeLoginCode(ctx, c.ID, secret, code, c.RedirectURI, "")
			results <- e == nil
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("exchange successes=%d", success)
	}
	code = newCode()
	_, e = r.st.DB().Exec(`UPDATE login_requests SET expires_at=? WHERE hash=?`, time.Now().Add(-time.Second).Unix(), store.LoginHash(code))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.st.ExchangeLoginCode(ctx, c.ID, secret, code, c.RedirectURI, ""); e == nil {
		t.Fatal("expired code accepted")
	}
	code = newCode()
	_, e = r.st.DB().Exec(`UPDATE users SET status='banned' WHERE id=?`, uid)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.st.ExchangeLoginCode(ctx, c.ID, secret, code, c.RedirectURI, ""); e == nil {
		t.Fatal("banned user accepted")
	}
	// Operational roles must not manage client secrets.
	op, _ := admin.NewUser("operator@example.com", "password123", "operator")
	r.st.CreateStaff(ctx, op)
	oper := &client{t: t, srv: r.srv}
	oper.do("POST", "/api/admin/login", map[string]string{"Email": op.Email, "Password": "password123"}, nil)
	if status, _, _ := oper.do("POST", "/api/admin/settings/login-clients", c, nil); status != 403 {
		t.Fatal("operator could create client")
	}
}

func cloneLoginQuery(v url.Values) url.Values {
	out := url.Values{}
	for k, values := range v {
		out[k] = append([]string(nil), values...)
	}
	return out
}

func TestLoginProviderGrantIsolation(t *testing.T) {
	r := newRigAt(t, "https://captain.example.com")
	ctx := context.Background()
	u, _ := admin.NewUser("identity@example.com", "password123", "user")
	if e := r.st.CreateUser(ctx, u); e != nil {
		t.Fatal(e)
	}
	sessions := &sessionAuth{store: r.st}
	s, e := sessions.Create(ctx, u.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	c, secret, e := r.st.SaveLoginClient(ctx, store.LoginClient{Name: "One", RedirectURI: "https://app.example.com/callback", Enabled: true}, true)
	if e != nil {
		t.Fatal(e)
	}
	other, otherSecret, e := r.st.SaveLoginClient(ctx, store.LoginClient{Name: "Two", RedirectURI: c.RedirectURI, Enabled: true}, true)
	if e != nil {
		t.Fatal(e)
	}
	request := func() string {
		t.Helper()
		ticket, e := r.st.NewLoginRequest(ctx, store.LoginRequest{ClientID: c.ID, SessionID: s.ID, RedirectURI: c.RedirectURI, Scope: "profile email", State: "state"})
		if e != nil {
			t.Fatal(e)
		}
		return ticket
	}
	mint := func() string {
		t.Helper()
		_, code, e := r.st.DecideLoginRequest(ctx, request(), s.ID, true)
		if e != nil {
			t.Fatal(e)
		}
		token, _, e := r.st.ExchangeLoginCode(ctx, c.ID, secret, code, c.RedirectURI, "")
		if e != nil {
			t.Fatal(e)
		}
		return token
	}
	expired := request()
	if _, e = r.st.DB().Exec(`UPDATE login_requests SET expires_at=0 WHERE hash=?`, store.LoginHash(expired)); e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.st.DecideLoginRequest(ctx, expired, s.ID, true); e == nil {
		t.Fatal("expired approval accepted")
	}
	_, code, e := r.st.DecideLoginRequest(ctx, request(), s.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.st.ExchangeLoginCode(ctx, other.ID, otherSecret, code, c.RedirectURI, ""); e == nil {
		t.Fatal("code used by another client")
	}
	token, _, e := r.st.ExchangeLoginCode(ctx, c.ID, secret, code, c.RedirectURI, "")
	if e != nil {
		t.Fatal(e)
	}
	first, e := r.st.LoginUserInfo(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.st.DB().Exec(`UPDATE users SET email='renamed@example.com',uuid='new-proxy-identity' WHERE id=?`, u.ID); e != nil {
		t.Fatal(e)
	}
	second, e := r.st.LoginUserInfo(ctx, token)
	if e != nil || first["sub"] != second["sub"] || second["email"] != "renamed@example.com" {
		t.Fatal("identity changed with email/UUID", e)
	}
	if _, e = r.st.DB().Exec(`UPDATE login_requests SET expires_at=0 WHERE hash=?`, store.LoginHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e = r.st.LoginUserInfo(ctx, token); e == nil {
		t.Fatal("expired token accepted")
	}
	token = mint()
	if e = r.st.DeleteUser(ctx, u.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = r.st.LoginUserInfo(ctx, token); e == nil {
		t.Fatal("deleted user accepted")
	}
	// A fresh account reusing the same email does not inherit the external subject.
	fresh, _ := admin.NewUser("identity@example.com", "password123", "user")
	if e = r.st.CreateUser(ctx, fresh); e != nil {
		t.Fatal(e)
	}
	s, e = sessions.Create(ctx, fresh.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	token = mint()
	third, e := r.st.LoginUserInfo(ctx, token)
	if e != nil || third["sub"] == first["sub"] {
		t.Fatal("recreated account inherited subject", e)
	}
	if _, e = r.st.DB().Exec(`UPDATE sessions SET expires_at=0 WHERE id=?`, s.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = r.st.LoginUserInfo(ctx, token); e == nil {
		t.Fatal("expired session accepted")
	}
	// Callback schemes and wildcard-like invalid URLs cannot be registered.
	for _, uri := range []string{"http://app.example.com/callback", "https://app.example.com/callback#fragment", "https://user@app.example.com/callback", "javascript:alert(1)"} {
		if _, _, e = r.st.SaveLoginClient(ctx, store.LoginClient{Name: "Bad", RedirectURI: uri, Enabled: true}, true); e == nil {
			t.Fatalf("invalid callback accepted: %s", uri)
		}
	}
}
