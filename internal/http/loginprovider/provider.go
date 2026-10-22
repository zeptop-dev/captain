// Package loginprovider supplies a confidential-client OAuth login bridge.
// It intentionally does not advertise OIDC or issue ID/refresh tokens.
package loginprovider

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/store"
)

type handlers struct {
	st    *store.Store
	base  string
	limit *ratelimit.Limiter
}

func Register(mux *http.ServeMux, st *store.Store, base string) {
	h := &handlers{st: st, base: strings.TrimRight(base, "/"), limit: &ratelimit.Limiter{Max: 60, Window: time.Minute, Lock: time.Minute}}
	for path, fn := range map[string]http.HandlerFunc{
		"GET /api/login-provider/authorize": h.authorize,
		"GET /api/login-provider/request":   h.request,
		"POST /api/login-provider/decision": h.decision,
		"POST /api/login-provider/token":    h.token,
		"GET /api/login-provider/userinfo":  h.userinfo,
	} {
		mux.HandleFunc(path, h.wrap(fn))
	}
}
func (h *handlers) wrap(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !store.ValidLoginURL(h.base) {
			fail(w, 503, "temporarily_unavailable")
			return
		}
		if ok, _ := h.limit.Allow(ratelimit.ClientIP(r)); !ok {
			fail(w, 429, "temporarily_unavailable")
			return
		}
		h.limit.Fail(ratelimit.ClientIP(r))
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		// The root HTTP handler holds Store.Accounts for this request.
		fn(w, r)
	}
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"error": code})
}
func (h *handlers) parse(r *http.Request) (store.LoginRequest, store.LoginClient, bool) {
	q := r.URL.Query()
	var out store.LoginRequest
	for _, v := range q {
		if len(v) != 1 {
			return out, store.LoginClient{}, false
		}
	}
	if len(r.URL.RawQuery) > 6000 || q.Get("response_type") != "code" || q.Get("state") == "" || len(q.Get("state")) > 512 {
		return out, store.LoginClient{}, false
	}
	c, e := h.st.LoginClient(r.Context(), q.Get("client_id"))
	if e != nil || !c.Enabled || c.RedirectURI != q.Get("redirect_uri") {
		return out, c, false
	}
	scope := strings.Fields(q.Get("scope"))
	if len(scope) == 0 {
		scope = []string{"profile"}
	}
	for _, s := range scope {
		if s != "profile" && s != "email" {
			return out, c, false
		}
	}
	if !slices.Contains(scope, "profile") {
		return out, c, false
	}
	challenge := q.Get("code_challenge")
	if challenge != "" {
		b, e := base64.RawURLEncoding.DecodeString(challenge)
		if e != nil || len(b) != 32 || q.Get("code_challenge_method") != "S256" {
			return out, c, false
		}
	} else if q.Get("code_challenge_method") != "" {
		return out, c, false
	}
	return store.LoginRequest{ClientID: c.ID, RedirectURI: c.RedirectURI, State: q.Get("state"), Scope: strings.Join(scope, " "), Challenge: challenge}, c, true
}
func (h *handlers) authorize(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.parse(r); !ok {
		fail(w, 400, "invalid_request")
		return
	}
	http.Redirect(w, r, h.base+"/portal/authorize?"+r.URL.RawQuery, http.StatusFound)
}
func (h *handlers) session(r *http.Request) string {
	c, e := r.Cookie("captain_session")
	if e != nil {
		return ""
	}
	u, staff, e := h.st.SessionUser(r.Context(), c.Value, time.Now())
	if e != nil || staff || u == nil || u.IsStaff() || u.Status != "active" {
		return ""
	}
	return c.Value
}
func (h *handlers) request(w http.ResponseWriter, r *http.Request) {
	v, c, ok := h.parse(r)
	if !ok {
		fail(w, 400, "invalid_request")
		return
	}
	v.SessionID = h.session(r)
	if v.SessionID == "" {
		fail(w, 401, "login_required")
		return
	}
	ticket, e := h.st.NewLoginRequest(r.Context(), v)
	if e != nil {
		fail(w, 400, "invalid_request")
		return
	}
	u, _, e := h.st.SessionUser(r.Context(), v.SessionID, time.Now())
	if e != nil || u == nil {
		fail(w, 401, "login_required")
		return
	}
	reply(w, 200, map[string]any{"account": u.Email, "ticket": ticket, "name": c.Name, "redirect_uri": c.RedirectURI, "scope": v.Scope})
}
func (h *handlers) decision(w http.ResponseWriter, r *http.Request) {
	// Browser approval requires the configured exact origin, not forwarded hosts.
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	u, e := url.Parse(src)
	base, _ := url.Parse(h.base)
	if e != nil || u.Host != base.Host || u.Scheme != base.Scheme {
		fail(w, 403, "access_denied")
		return
	}
	var v struct {
		Ticket string `json:"ticket"`
		Allow  bool   `json:"allow"`
	}
	if json.NewDecoder(r.Body).Decode(&v) != nil || len(v.Ticket) > 128 {
		fail(w, 400, "invalid_request")
		return
	}
	session := h.session(r)
	if session == "" {
		fail(w, 401, "login_required")
		return
	}
	req, code, e := h.st.DecideLoginRequest(r.Context(), v.Ticket, session, v.Allow)
	if e != nil {
		fail(w, 400, "invalid_request")
		return
	}
	dest, _ := url.Parse(req.RedirectURI)
	q := dest.Query()
	q.Set("state", req.State)
	if v.Allow {
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	dest.RawQuery = q.Encode()
	reply(w, 200, map[string]string{"redirect": dest.String()})
}
func (h *handlers) token(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") || r.ParseForm() != nil {
		fail(w, 400, "invalid_request")
		return
	}
	for _, v := range r.PostForm {
		if len(v) != 1 {
			fail(w, 400, "invalid_request")
			return
		}
	}
	v := r.PostForm
	client, secret := v.Get("client_id"), v.Get("client_secret")
	if r.Header.Get("Authorization") != "" {
		a, b, ok := r.BasicAuth()
		if !ok || client != "" || secret != "" {
			fail(w, 400, "invalid_request")
			return
		}
		client, secret = a, b
	}
	if client == "" || secret == "" || len(client) > 128 || len(secret) > 128 {
		fail(w, 401, "invalid_client")
		return
	}
	if v.Get("grant_type") != "authorization_code" {
		fail(w, 400, "unsupported_grant_type")
		return
	}
	challenge := ""
	if verifier := v.Get("code_verifier"); verifier != "" {
		if len(verifier) < 43 || len(verifier) > 128 || strings.IndexFunc(verifier, func(c rune) bool {
			return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~", c)
		}) >= 0 {
			fail(w, 400, "invalid_grant")
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	token, scope, e := h.st.ExchangeLoginCode(r.Context(), client, secret, v.Get("code"), v.Get("redirect_uri"), challenge)
	if e != nil {
		if e == store.ErrLoginClient {
			w.Header().Set("WWW-Authenticate", `Basic realm="Captain login"`)
			fail(w, 401, "invalid_client")
		} else {
			fail(w, 400, "invalid_grant")
		}
		return
	}
	reply(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 300, "scope": scope})
}
func (h *handlers) userinfo(w http.ResponseWriter, r *http.Request) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 128 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, 401, "invalid_token")
		return
	}
	out, e := h.st.LoginUserInfo(r.Context(), parts[1])
	if e != nil {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		fail(w, 401, "invalid_token")
		return
	}
	reply(w, 200, out)
}
