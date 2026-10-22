package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
)

var ErrLoginGrant = errors.New("invalid or expired login grant")
var ErrLoginClient = errors.New("invalid login application")

type LoginClient struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RedirectURI string `json:"redirect_uri"`
	Enabled     bool   `json:"enabled"`
}

func LoginHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Only HTTPS callbacks are supported, except explicit loopback development URLs.
func ValidLoginURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\\") && (u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}
func (s *Store) LoginClients(ctx context.Context) ([]LoginClient, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,name,redirect_uri,enabled FROM login_clients ORDER BY name,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []LoginClient{}
	for rows.Next() {
		var c LoginClient
		if e = rows.Scan(&c.ID, &c.Name, &c.RedirectURI, &c.Enabled); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) LoginClient(ctx context.Context, id string) (LoginClient, error) {
	var c LoginClient
	e := s.db.QueryRowContext(ctx, `SELECT id,name,redirect_uri,enabled FROM login_clients WHERE id=?`, id).Scan(&c.ID, &c.Name, &c.RedirectURI, &c.Enabled)
	return c, wrapNotFound(e)
}
func (s *Store) SaveLoginClient(ctx context.Context, c LoginClient, create bool) (LoginClient, string, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 100 || strings.ContainsAny(c.Name, "\r\n") || len(c.RedirectURI) > 2048 || !ValidLoginURL(c.RedirectURI) {
		return c, "", ErrLoginClient
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return c, "", e
	}
	defer tx.Rollback()
	secret := ""
	if create {
		c.ID = auth.Token(18)
		secret = auth.Token(32)
		_, e = tx.ExecContext(ctx, `INSERT INTO login_clients(id,name,secret_hash,redirect_uri,enabled) VALUES(?,?,?,?,?)`, c.ID, c.Name, LoginHash(secret), c.RedirectURI, c.Enabled)
	} else {
		var n int
		e = tx.QueryRowContext(ctx, `SELECT 1 FROM login_clients WHERE id=?`, c.ID).Scan(&n)
		if e == nil {
			_, e = tx.ExecContext(ctx, `DELETE FROM login_requests WHERE client_id=?`, c.ID)
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE login_clients SET name=?,redirect_uri=?,enabled=? WHERE id=?`, c.Name, c.RedirectURI, c.Enabled, c.ID)
		}
	}
	if e != nil {
		return c, "", wrapNotFound(e)
	}
	return c, secret, tx.Commit()
}
func (s *Store) RotateLoginClient(ctx context.Context, id string) (string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	secret := auth.Token(32)
	res, e := tx.ExecContext(ctx, `UPDATE login_clients SET secret_hash=? WHERE id=?`, LoginHash(secret), id)
	if e != nil {
		return "", e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return "", ErrNotFound
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM login_requests WHERE client_id=?`, id); e != nil {
		return "", e
	}
	return secret, tx.Commit()
}
func (s *Store) DeleteLoginClient(ctx context.Context, id string) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM login_clients WHERE id=?`, id)
	return e
}

type LoginRequest struct{ ClientID, SessionID, RedirectURI, State, Scope, Challenge string }

func (s *Store) NewLoginRequest(ctx context.Context, r LoginRequest) (string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	// Validate live account, session and callback together with the write.
	var uid int64
	e = tx.QueryRowContext(ctx, `SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id JOIN login_clients c ON c.id=? WHERE s.id=? AND s.admin=0 AND s.expires_at>? AND u.status='active' AND u.role='user' AND c.enabled=1 AND c.redirect_uri=?`, r.ClientID, r.SessionID, now(), r.RedirectURI).Scan(&uid)
	if e != nil {
		return "", ErrLoginGrant
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM login_requests WHERE expires_at<=?`, now()); e != nil {
		return "", e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_requests WHERE session_id=? AND stage='pending'`, r.SessionID).Scan(&count); e != nil {
		return "", e
	}
	if count >= 20 {
		return "", ErrLoginGrant
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO login_subjects(user_id,subject) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, uid, auth.Token(24)); e != nil {
		return "", e
	}
	ticket := auth.Token(32)
	_, e = tx.ExecContext(ctx, `INSERT INTO login_requests(hash,client_id,session_id,stage,redirect_uri,state,scope,challenge,expires_at) VALUES(?,?,?,'pending',?,?,?,?,?)`, LoginHash(ticket), r.ClientID, r.SessionID, r.RedirectURI, r.State, r.Scope, r.Challenge, time.Now().Add(10*time.Minute).Unix())
	if e != nil {
		return "", e
	}
	return ticket, tx.Commit()
}
func (s *Store) DecideLoginRequest(ctx context.Context, ticket, session string, allow bool) (LoginRequest, string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return LoginRequest{}, "", e
	}
	defer tx.Rollback()
	var r LoginRequest
	e = tx.QueryRowContext(ctx, `SELECT r.client_id,r.session_id,r.redirect_uri,r.state,r.scope,r.challenge FROM login_requests r JOIN login_clients c ON c.id=r.client_id JOIN sessions s ON s.id=r.session_id JOIN users u ON u.id=s.user_id WHERE r.hash=? AND r.stage='pending' AND r.session_id=? AND r.expires_at>? AND s.expires_at>? AND s.admin=0 AND u.status='active' AND u.role='user' AND c.enabled=1 AND c.redirect_uri=r.redirect_uri`, LoginHash(ticket), session, now(), now()).Scan(&r.ClientID, &r.SessionID, &r.RedirectURI, &r.State, &r.Scope, &r.Challenge)
	if e != nil {
		return r, "", ErrLoginGrant
	}
	code := ""
	if allow {
		code = auth.Token(32)
		_, e = tx.ExecContext(ctx, `UPDATE login_requests SET hash=?,stage='code',expires_at=? WHERE hash=?`, LoginHash(code), time.Now().Add(time.Minute).Unix(), LoginHash(ticket))
	} else {
		_, e = tx.ExecContext(ctx, `DELETE FROM login_requests WHERE hash=?`, LoginHash(ticket))
	}
	if e != nil {
		return r, "", e
	}
	return r, code, tx.Commit()
}
func (s *Store) ExchangeLoginCode(ctx context.Context, client, secret, code, redirect, challenge string) (string, string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback()
	var hash string
	e = tx.QueryRowContext(ctx, `SELECT secret_hash FROM login_clients WHERE id=? AND enabled=1`, client).Scan(&hash)
	if e != nil || subtle.ConstantTimeCompare([]byte(hash), []byte(LoginHash(secret))) != 1 {
		return "", "", ErrLoginClient
	}
	var scope, expected string
	e = tx.QueryRowContext(ctx, `SELECT r.scope,r.challenge FROM login_requests r JOIN sessions s ON s.id=r.session_id JOIN users u ON u.id=s.user_id JOIN login_clients c ON c.id=r.client_id WHERE r.hash=? AND r.stage='code' AND r.client_id=? AND r.redirect_uri=? AND r.expires_at>? AND s.expires_at>? AND s.admin=0 AND u.status='active' AND u.role='user' AND c.redirect_uri=r.redirect_uri`, LoginHash(code), client, redirect, now(), now()).Scan(&scope, &expected)
	if e != nil || (expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1) {
		return "", "", ErrLoginGrant
	}
	token := auth.Token(32)
	_, e = tx.ExecContext(ctx, `UPDATE login_requests SET hash=?,stage='token',expires_at=? WHERE hash=?`, LoginHash(token), time.Now().Add(5*time.Minute).Unix(), LoginHash(code))
	if e != nil {
		return "", "", e
	}
	return token, scope, tx.Commit()
}
func (s *Store) LoginUserInfo(ctx context.Context, token string) (map[string]any, error) {
	var subject, email, scope string
	e := s.db.QueryRowContext(ctx, `SELECT i.subject,u.email,r.scope FROM login_requests r JOIN login_clients c ON c.id=r.client_id JOIN sessions s ON s.id=r.session_id JOIN users u ON u.id=s.user_id JOIN login_subjects i ON i.user_id=u.id WHERE r.hash=? AND r.stage='token' AND r.expires_at>? AND c.enabled=1 AND c.redirect_uri=r.redirect_uri AND s.expires_at>? AND s.admin=0 AND u.status='active' AND u.role='user'`, LoginHash(token), now(), now()).Scan(&subject, &email, &scope)
	if e != nil {
		return nil, ErrLoginGrant
	}
	out := map[string]any{"sub": subject, "preferred_username": "cap_" + subject[:12], "name": "Captain user"}
	if strings.Contains(" "+scope+" ", " email ") {
		out["email"] = email
		out["email_verified"] = false
	}
	return out, nil
}
