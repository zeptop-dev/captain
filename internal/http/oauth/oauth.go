// Package oauth signs users in through OpenID Connect providers (Casdoor,
// Authentik, Google, ...): the portal shows one button per configured
// provider, the callback links the provider subject to a Captain user and
// starts a normal session.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zeptop-dev/captain/internal/webhook"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

const (
	flowCookie    = "captain_oauth"
	sessionCookie = "captain_session"
	flowTTL       = 10 * time.Minute
)

// Deps wires the handlers.
type Deps struct {
	Hooks    *webhook.Hub // nil = no webhooks
	Store    *store.Store
	Sessions admin.SessionStore
	Log      *slog.Logger
	BaseURL  string // public URL, for the redirect URI
	Secure   bool
	// Registration mirrors the portal setting: password registration open.
	Registration bool
	// Resolve returns the signed-in user for a request, or nil.
	Resolve func(r *http.Request) *domain.User
}

type handlers struct {
	Deps
	mu        sync.Mutex
	providers map[string]*oidc.Provider // issuer -> discovered provider
}

// Register mounts the routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{Deps: d, providers: map[string]*oidc.Provider{}}
	mux.HandleFunc("GET /api/oauth/providers", h.list)
	mux.HandleFunc("GET /api/oauth/{id}/start", h.start)
	mux.HandleFunc("GET /api/oauth/{id}/callback", h.callback)
	mux.HandleFunc("GET /api/oauth/identities", h.identities)
	mux.HandleFunc("DELETE /api/oauth/identities/{id}", h.unlink)
}

func (h *handlers) settings(ctx context.Context) store.OIDCSettings {
	var s store.OIDCSettings
	_ = h.Store.GetSetting(ctx, store.SettingOIDC, &s)
	return s
}

func (h *handlers) provider(ctx context.Context, id string) (*store.OIDCProvider, error) {
	for _, p := range h.settings(ctx).Providers {
		if p.ID == id {
			p := p
			if len(p.Scopes) == 0 {
				p.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
			}
			return &p, nil
		}
	}
	return nil, errors.New("unknown login provider")
}

// discover caches the OIDC discovery document per issuer.
func (h *handlers) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	h.mu.Lock()
	p := h.providers[issuer]
	h.mu.Unlock()
	if p != nil {
		return p, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, strings.TrimRight(issuer, "/"))
	if err != nil {
		return nil, fmt.Errorf("provider discovery failed: %w", err)
	}
	h.mu.Lock()
	h.providers[issuer] = p
	h.mu.Unlock()
	return p, nil
}

func (h *handlers) redirectURI(id string) string {
	return strings.TrimRight(h.BaseURL, "/") + "/api/oauth/" + id + "/callback"
}

// PublicProvider is what the login page needs.
type PublicProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	s := h.settings(r.Context())
	out := []PublicProvider{}
	for _, p := range s.Providers {
		if p.Issuer != "" && p.ClientID != "" {
			out = append(out, PublicProvider{ID: p.ID, Name: p.Name})
		}
	}
	pw := s.PasswordLogin == nil || *s.PasswordLogin || len(out) == 0
	writeJSON(w, http.StatusOK, map[string]any{"providers": out, "password_login": pw})
}

// flow is what survives the round trip to the provider, in a cookie.
type flow struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"next"`
	Link     bool   `json:"link"` // attach to the signed-in user instead of signing in
}

func (h *handlers) start(w http.ResponseWriter, r *http.Request) {
	p, err := h.provider(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	prov, err := h.discover(r.Context(), p.Issuer)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/portal/"
	}
	f := flow{Provider: p.ID, State: auth.Token(16), Nonce: auth.Token(16), Verifier: oauth2.GenerateVerifier(), Next: next,
		Link: r.URL.Query().Get("link") == "1" && h.Resolve(r) != nil}
	raw, _ := json.Marshal(f)
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: base64.RawURLEncoding.EncodeToString(raw), Path: "/api/oauth/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(flowTTL.Seconds())})
	cfg := oauth2.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: prov.Endpoint(), RedirectURL: h.redirectURI(p.ID), Scopes: p.Scopes}
	http.Redirect(w, r, cfg.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

type claims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	// Casdoor puts its user object's field here.
	CasdoorVerified bool `json:"emailVerified"`
}

func (h *handlers) callback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := r.Cookie(flowCookie)
	if err != nil {
		h.fail(w, r, errors.New("login flow expired; try again"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/api/oauth/", MaxAge: -1})
	raw, _ := base64.RawURLEncoding.DecodeString(c.Value)
	var f flow
	if json.Unmarshal(raw, &f) != nil || f.Provider != id || f.State == "" || r.URL.Query().Get("state") != f.State {
		h.fail(w, r, errors.New("login state mismatch; try again"))
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		h.fail(w, r, fmt.Errorf("provider refused: %s %s", e, r.URL.Query().Get("error_description")))
		return
	}
	p, err := h.provider(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	prov, err := h.discover(r.Context(), p.Issuer)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cfg := oauth2.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: prov.Endpoint(), RedirectURL: h.redirectURI(p.ID), Scopes: p.Scopes}
	tok, err := cfg.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		h.fail(w, r, fmt.Errorf("token exchange failed: %w", err))
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		h.fail(w, r, errors.New("provider returned no id_token"))
		return
	}
	idt, err := prov.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, rawID)
	if err != nil {
		h.fail(w, r, fmt.Errorf("id_token rejected: %w", err))
		return
	}
	if idt.Nonce != f.Nonce {
		h.fail(w, r, errors.New("nonce mismatch"))
		return
	}
	var cl claims
	if err := idt.Claims(&cl); err != nil || cl.Subject == "" {
		h.fail(w, r, errors.New("id_token has no subject"))
		return
	}
	// Some providers only put email in userinfo.
	if cl.Email == "" {
		if ui, err := prov.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
			var extra claims
			_ = ui.Claims(&extra)
			cl.Email, cl.EmailVerified = ui.Email, ui.EmailVerified
			if extra.CasdoorVerified {
				cl.CasdoorVerified = true
			}
		}
	}
	email := strings.ToLower(strings.TrimSpace(cl.Email))
	verified := cl.EmailVerified || cl.CasdoorVerified || p.TrustEmail

	user, err := h.resolveUser(ctx, r, p, cl.Subject, email, verified, f.Link)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if user.Status != "active" {
		h.fail(w, r, errors.New("account disabled"))
		return
	}
	if strings.HasPrefix(f.Next, "/admin") && !user.IsStaff() {
		h.fail(w, r, errors.New("admin only"))
		return
	}
	if strings.HasPrefix(f.Next, "/admin") && user.IsStaff() {
		// OIDC may open the console only for staff without an
		// authenticator; with TOTP on, the admin login is the only door.
		if _, enabled, _ := h.Store.TOTP(ctx, user.ID); enabled {
			h.fail(w, r, errors.New("this account uses an authenticator: sign in with password and code"))
			return
		}
	}
	if !f.Link {
		sess, err := h.Sessions.Create(ctx, user.ID, strings.HasPrefix(f.Next, "/admin") && user.IsStaff())
		if err != nil {
			h.fail(w, r, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sess.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.Secure, Expires: sess.ExpiresAt})
	}
	http.Redirect(w, r, f.Next, http.StatusFound)
}

// resolveUser maps a provider subject to a Captain user: an existing link,
// else the signed-in user when linking, else a verified-email match, else a
// new account when registration allows it.
func (h *handlers) resolveUser(ctx context.Context, r *http.Request, p *store.OIDCProvider, subject, email string, verified, link bool) (*domain.User, error) {
	if link {
		u := h.Resolve(r)
		if u == nil {
			return nil, errors.New("sign in first to link a login")
		}
		if other, err := h.Store.UserByIdentity(ctx, p.ID, subject); err == nil && other != u.ID {
			return nil, errors.New("this external account is already linked to another user")
		}
		return u, h.Store.LinkIdentity(ctx, u.ID, p.ID, subject, email)
	}
	if uid, err := h.Store.UserByIdentity(ctx, p.ID, subject); err == nil {
		u, err := h.Store.UserByID(ctx, uid)
		if err != nil {
			return nil, err
		}
		// Follow an email change at the provider, unless the new address
		// already belongs to another account.
		if verified && email != "" && email != u.Email {
			if _, taken := h.Store.UserByEmail(ctx, email); taken != nil {
				if err := h.Store.UpdateUserEmail(ctx, u.ID, email); err == nil {
					u.Email = email
					_ = h.Store.LinkIdentity(ctx, u.ID, p.ID, subject, email)
				}
			}
		}
		return u, nil
	}
	if email != "" && verified {
		if u, err := h.Store.UserByEmail(ctx, email); err == nil {
			return u, h.Store.LinkIdentity(ctx, u.ID, p.ID, subject, email)
		}
	}
	if !h.Registration && !p.AutoRegister {
		return nil, errors.New("no account for this login and registration is closed")
	}
	var reg store.RegistrationSettings
	_ = h.Store.GetSetting(ctx, store.SettingRegistration, &reg)
	if !reg.EmailAllowed(email) {
		return nil, errors.New("this email domain is not accepted")
	}
	if reg.InviteOnly {
		if c, err := r.Cookie("captain_ref"); err != nil || c.Value == "" {
			return nil, errors.New("registration requires an invite link")
		}
	}
	if email == "" {
		return nil, errors.New("the provider did not share an email address; cannot create an account")
	}
	if !verified {
		return nil, errors.New("the provider did not verify the email address")
	}
	u, err := admin.NewUser(email, auth.Token(24), "user")
	if err != nil {
		return nil, err
	}
	if c, err := r.Cookie("captain_ref"); err == nil && c.Value != "" {
		if inviter, err := h.Store.UserByInviteCode(ctx, c.Value); err == nil {
			u.InvitedBy = &inviter.ID
		}
	}
	if err := h.Store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	if err := h.Store.ApplyTrial(ctx, u.ID, time.Now()); err != nil {
		h.Log.Warn("trial grant", "user", u.ID, "err", err)
	}
	h.Hooks.Emit(ctx, webhook.UserRegistered, map[string]any{"user_id": u.ID, "email": u.Email, "invited_by": u.InvitedBy, "method": "oidc:" + p.ID})
	return u, h.Store.LinkIdentity(ctx, u.ID, p.ID, subject, email)
}

// identities lists the signed-in user's external logins.
func (h *handlers) identities(w http.ResponseWriter, r *http.Request) {
	u := h.Resolve(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	list, err := h.Store.IdentitiesByUser(r.Context(), u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *handlers) unlink(w http.ResponseWriter, r *http.Request) {
	u := h.Resolve(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	if err := h.Store.UnlinkIdentity(r.Context(), u.ID, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// fail sends the user back to the login page with the reason.
func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Warn("oauth login failed", "err", err)
	http.Redirect(w, r, "/portal/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
