package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
)

// RegistrationLegal is opt-in; links point to documents managed by the operator.
// Version must change whenever the documents behind these links change.
type RegistrationLegal struct {
	TermsURL        string `json:"terms_url"`
	PrivacyURL      string `json:"privacy_url"`
	DocumentVersion string `json:"document_version"`
}

func (l *RegistrationLegal) Version() string {
	if l == nil || (l.TermsURL == "" && l.PrivacyURL == "") {
		return ""
	}
	raw, _ := json.Marshal(l)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func (l *RegistrationLegal) Validate() error {
	if l == nil {
		return nil
	}
	l.TermsURL, l.PrivacyURL, l.DocumentVersion = strings.TrimSpace(l.TermsURL), strings.TrimSpace(l.PrivacyURL), strings.TrimSpace(l.DocumentVersion)
	if len(l.DocumentVersion) > 128 || strings.ContainsAny(l.DocumentVersion, "\r\n") {
		return ErrRegistrationLegal
	}
	for _, v := range []string{l.TermsURL, l.PrivacyURL} {
		if v == "" {
			continue
		}
		u, e := url.Parse(v)
		if e != nil || len(v) > 2048 || strings.ContainsAny(v, "\r\n\\") || u.User != nil || !((u.Scheme == "https" && u.Host != "") || (u.Scheme == "" && u.Host == "" && strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//"))) {
			return ErrRegistrationLegal
		}
	}
	if l.Version() != "" && l.DocumentVersion == "" {
		return ErrRegistrationLegal
	}
	return nil
}

var ErrRegistrationLegal = errors.New("registration terms must be accepted at the current version")

// RegistrationProof is server-side evidence, never decoded directly from a browser.
type RegistrationProof struct {
	Code           string
	VerifyCode     bool
	VerifiedSource string
	Accepted       bool
	LegalVersion   string
	OAuthState     string
	Method         string
}

func (s *Store) SaveOAuthRegistrationConsent(ctx context.Context, state, provider, version string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM registration_oauth_consents WHERE expires_at<=?`, now()); e != nil {
		return e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM registration_oauth_consents`).Scan(&count); e != nil {
		return e
	}
	if count >= 10000 {
		return errors.New("too many pending registration requests")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO registration_oauth_consents(state_hash,provider,version,accepted_at,expires_at) VALUES(?,?,?,?,?)`, auth.SHA256Hex(state), provider, version, now(), time.Now().Add(10*time.Minute).Unix())
	if e != nil {
		return e
	}
	return tx.Commit()
}

// CreateRegisteredUser commits identity, code consumption, verification evidence
// and legal receipt together. Old users are never retroactively marked verified.
func (s *Store) CreateRegisteredUser(ctx context.Context, u *domain.User, p RegistrationProof) error {
	if u.Role == "" {
		u.Role = domain.RoleUser
	}
	if u.Role != domain.RoleUser {
		return errors.New("customer role must be user")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var reg RegistrationSettings
	var raw string
	e = tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingRegistration).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if raw != "" {
		if e = json.Unmarshal([]byte(raw), &reg); e != nil {
			return e
		}
	}
	version := reg.Legal.Version()
	acceptedAt := now()
	if version != "" {
		if p.OAuthState != "" {
			e = tx.QueryRowContext(ctx, `SELECT accepted_at FROM registration_oauth_consents WHERE state_hash=? AND provider=? AND version=? AND expires_at>?`, auth.SHA256Hex(p.OAuthState), strings.TrimPrefix(p.Method, "oidc:"), version, now()).Scan(&acceptedAt)
			if errors.Is(e, sql.ErrNoRows) {
				return ErrRegistrationLegal
			}
			if e != nil {
				return e
			}
		} else if !p.Accepted || p.LegalVersion != version {
			return ErrRegistrationLegal
		}
	}
	if p.VerifyCode {
		if e = checkCodeTx(ctx, tx, u.Email, "register", p.Code, true); e != nil {
			if ce := tx.Commit(); ce != nil {
				return ce
			}
			return e
		}
	}
	if u.InviteCode == "" {
		u.InviteCode = newInviteCode()
	}
	if e = createUserTx(ctx, tx, u, 0, now()); e != nil {
		return e
	}
	source := p.VerifiedSource
	if p.VerifyCode {
		source = "email_code"
	}
	if source != "" {
		if _, e = tx.ExecContext(ctx, `INSERT INTO user_email_verifications(user_id,email,source,verified_at) VALUES(?,?,?,?)`, u.ID, u.Email, source, now()); e != nil {
			return e
		}
	}
	if version != "" {
		if _, e = tx.ExecContext(ctx, `INSERT INTO registration_receipts(user_id,version,terms_url,privacy_url,document_version,method,accepted_at) VALUES(?,?,?,?,?,?,?)`, u.ID, version, reg.Legal.TermsURL, reg.Legal.PrivacyURL, reg.Legal.DocumentVersion, p.Method, acceptedAt); e != nil {
			return e
		}
	}
	if p.OAuthState != "" {
		if _, e = tx.ExecContext(ctx, `DELETE FROM registration_oauth_consents WHERE state_hash=?`, auth.SHA256Hex(p.OAuthState)); e != nil {
			return e
		}
	}
	return tx.Commit()
}

type EmailVerification struct {
	Verified   bool       `json:"verified"`
	VerifiedAt *time.Time `json:"verified_at"`
	Source     string     `json:"source"`
}

func (s *Store) EmailVerification(ctx context.Context, id int64) (EmailVerification, error) {
	var v EmailVerification
	var at int64
	e := s.db.QueryRowContext(ctx, `SELECT v.verified_at,v.source FROM user_email_verifications v JOIN users u ON u.id=v.user_id AND u.email=v.email WHERE u.id=?`, id).Scan(&at, &v.Source)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	t := unix(at)
	v.Verified = true
	v.VerifiedAt = &t
	return v, nil
}
func (s *Store) RecordVerifiedEmail(ctx context.Context, id int64, email, source string) error {
	res, e := s.db.ExecContext(ctx, `INSERT INTO user_email_verifications(user_id,email,source,verified_at) SELECT id,email,?,? FROM users WHERE id=? AND email=? ON CONFLICT(user_id) DO UPDATE SET email=excluded.email,source=excluded.source,verified_at=excluded.verified_at`, source, now(), id, email)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n == 0 {
		return ErrNotFound
	}
	return e
}
func EmailVerificationPurpose(agentID int64) string { return fmt.Sprintf("verify-email:%d", agentID) }
func (s *Store) VerifyUserEmail(ctx context.Context, id int64, email, code string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var agentID int64
	if e = tx.QueryRowContext(ctx, `SELECT agent_id FROM users WHERE id=? AND email=? AND status='active'`, id, email).Scan(&agentID); e != nil {
		return wrapNotFound(e)
	}
	if e = checkCodeTx(ctx, tx, email, EmailVerificationPurpose(agentID), code, true); e != nil {
		if ce := tx.Commit(); ce != nil {
			return ce
		}
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO user_email_verifications(user_id,email,source,verified_at) VALUES(?,?,'email_code',?) ON CONFLICT(user_id) DO UPDATE SET email=excluded.email,source=excluded.source,verified_at=excluded.verified_at`, id, email, now()); e != nil {
		return e
	}
	return tx.Commit()
}
