package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	chttp "github.com/zeptop-dev/captain/internal/http"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS, auth (auto-approves),
// token (signs an id_token for the configured user).
type fakeIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	email string
	sub   string
	nonce string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIdP{key: key, email: "alice@idp.test", sub: "user-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/auth", "token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks", "userinfo_endpoint": f.srv.URL + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		f.nonce = r.URL.Query().Get("nonce")
		u, _ := url.Parse(r.URL.Query().Get("redirect_uri"))
		q := u.Query()
		q.Set("code", "good-code")
		q.Set("state", r.URL.Query().Get("state"))
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		cl := map[string]any{"iss": f.srv.URL, "aud": "captain-client", "sub": f.sub, "email": f.email, "email_verified": true, "nonce": f.nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
		raw, _ := jwt.Signed(signer).Claims(cl).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw, "expires_in": 3600})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestOIDCLogin(t *testing.T) {
	idp := newFakeIdP(t)
	cfg := config.Default()
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.BaseURL = srv.URL
		chttp.New(cfg, st, slog.Default()).Handler().ServeHTTP(w, r)
	}))
	defer srv.Close()
	cfg.BaseURL = srv.URL
	_ = st.SetSetting(context.Background(), store.SettingOIDC, store.OIDCSettings{Providers: []store.OIDCProvider{{ID: "idp", Name: "Fake", Issuer: idp.srv.URL, ClientID: "captain-client", ClientSecret: "s", TrustEmail: true}}})

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	// The login page sees the provider.
	resp, _ := c.Get(srv.URL + "/api/oauth/providers")
	var list struct {
		Providers []struct{ ID, Name string }
	}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Providers) != 1 || list.Providers[0].ID != "idp" {
		t.Fatalf("providers: %+v", list)
	}
	// Full round trip: start -> IdP -> callback -> session cookie -> /api/portal/me.
	resp, err := c.Get(srv.URL + "/api/oauth/idp/start?next=/portal/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request.URL.Path != "/portal/" {
		t.Fatalf("expected to land on /portal/, got %s", resp.Request.URL)
	}
	resp, _ = c.Get(srv.URL + "/api/portal/me")
	if resp.StatusCode != 200 {
		t.Fatalf("not signed in after oidc: %d", resp.StatusCode)
	}
	var me struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&me)
	if me.Email != "alice@idp.test" {
		t.Fatalf("me: %+v", me)
	}
	uid, err := st.UserByIdentity(context.Background(), "idp", "user-1")
	if err != nil {
		t.Fatal("identity should be linked")
	}
	// Same subject with a changed email still maps to the same user.
	idp.email = "renamed@idp.test"
	jar2, _ := cookiejar.New(nil)
	c2 := &http.Client{Jar: jar2}
	c2.Get(srv.URL + "/api/oauth/idp/start")
	resp, _ = c2.Get(srv.URL + "/api/portal/me")
	var me2 struct {
		ID    int64
		Email string
	}
	json.NewDecoder(resp.Body).Decode(&me2)
	if me2.ID != uid {
		t.Fatalf("subject should map to the same user: %d vs %d", me2.ID, uid)
	}
	if me2.Email != "renamed@idp.test" {
		t.Fatalf("a verified email change at the provider should follow: %s", me2.Email)
	}
	// Existing password account with the provider's (verified) email gets linked, not duplicated.
	idp.sub, idp.email = "user-2", "admin@test"
	jar3, _ := cookiejar.New(nil)
	c3 := &http.Client{Jar: jar3}
	c3.Get(srv.URL + "/api/oauth/idp/start?next=/admin/")
	resp, _ = c3.Get(srv.URL + "/api/admin/me")
	if resp.StatusCode != 200 {
		t.Fatalf("admin should be signed in through oidc: %d", resp.StatusCode)
	}
	// Registration closed and unknown user: refused with a message on the login page.
	cfg.Portal.Registration = false
	idp.sub, idp.email = "user-3", "stranger@idp.test"
	jar4, _ := cookiejar.New(nil)
	c4 := &http.Client{Jar: jar4}
	resp, _ = c4.Get(srv.URL + "/api/oauth/idp/start")
	if !strings.Contains(resp.Request.URL.String(), "/portal/login?error=") {
		t.Fatalf("closed registration should bounce to login with an error: %s", resp.Request.URL)
	}
	_ = fmt.Sprint
}
