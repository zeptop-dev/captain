package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/testdb"
)

func TestRegistrationIdentityTransactions(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	newUser := func(email string) *domain.User {
		return &domain.User{Email: email, PasswordHash: "hash", Role: "user", UUID: email, SubToken: email, Status: "active"}
	}
	legal := &RegistrationLegal{TermsURL: "/terms", PrivacyURL: "https://example.com/privacy", DocumentVersion: "v1"}
	if e := s.SetSetting(ctx, SettingRegistration, RegistrationSettings{Legal: legal}); e != nil {
		t.Fatal(e)
	}
	code, e := s.NewCode(ctx, "new@example.com", "register", 10*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ValidateCode(ctx, "new@example.com", "register", code); e != nil {
		t.Fatal(e)
	}
	if e = s.ValidateCode(ctx, "new@example.com", "register", code); e != nil {
		t.Fatal("validation consumed challenge", e)
	}
	p := RegistrationProof{VerifyCode: true, Code: code, Method: "password"}
	if e = s.CreateRegisteredUser(ctx, newUser("new@example.com"), p); !errors.Is(e, ErrRegistrationLegal) {
		t.Fatal(e)
	}
	// Rejected consent does not consume a valid code.
	p.Accepted, p.LegalVersion = true, legal.Version()
	u := newUser("new@example.com")
	if e = s.CreateRegisteredUser(ctx, u, p); e != nil {
		t.Fatal(e)
	}
	v, e := s.EmailVerification(ctx, u.ID)
	if e != nil || !v.Verified || v.Source != "email_code" {
		t.Fatal(v, e)
	}
	var version string
	if e = s.db.QueryRow(`SELECT version FROM registration_receipts WHERE user_id=?`, u.ID).Scan(&version); e != nil || version != legal.Version() {
		t.Fatal(version, e)
	}
	if e = s.CheckCode(ctx, u.Email, "register", code); !errors.Is(e, ErrCodeInvalid) {
		t.Fatal("replayed code", e)
	}
	old := newUser("old@example.com")
	if e = s.CreateUser(ctx, old); e != nil {
		t.Fatal(e)
	}
	v, e = s.EmailVerification(ctx, old.ID)
	if e != nil || v.Verified {
		t.Fatal("old account marked verified", v, e)
	}
	if e = s.RecordVerifiedEmail(ctx, u.ID, "other@example.com", "email_code"); !errors.Is(e, ErrNotFound) {
		t.Fatal("mismatched proof accepted", e)
	}
	if e = s.UpdateUserEmail(ctx, u.ID, "changed@example.com"); e != nil {
		t.Fatal(e)
	}
	v, e = s.EmailVerification(ctx, u.ID)
	if e != nil || v.Verified {
		t.Fatal("email change kept proof", v, e)
	}
	// Challenges are bound to the immutable account identity, not just email.
	code, e = s.NewCode(ctx, old.Email, EmailVerificationPurpose(old.AgentID), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyUserEmail(ctx, u.ID, "changed@example.com", code); !errors.Is(e, ErrCodeInvalid) {
		t.Fatal(e)
	}
	if e = s.VerifyUserEmail(ctx, old.ID, old.Email, code); e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyUserEmail(ctx, old.ID, old.Email, code); !errors.Is(e, ErrCodeInvalid) {
		t.Fatal("replayed email verification", e)
	}
	if e = s.ChangeAccountID(ctx, old.ID, 100, false); e != nil {
		t.Fatal(e)
	}
	v, e = s.EmailVerification(ctx, 100)
	if e != nil || !v.Verified {
		t.Fatal("renumber lost proof", v, e)
	}
	// Legal proofs must come from a server-recorded OAuth flow and current version.
	oauth := newUser("oauth@example.com")
	op := RegistrationProof{Method: "oidc:example", OAuthState: "flow", VerifiedSource: "oidc:example", Accepted: true, LegalVersion: legal.Version()}
	if e = s.CreateRegisteredUser(ctx, oauth, op); !errors.Is(e, ErrRegistrationLegal) {
		t.Fatal("forged flow accepted", e)
	}
	if e = s.SaveOAuthRegistrationConsent(ctx, "flow", "example", legal.Version()); e != nil {
		t.Fatal(e)
	}
	legal.DocumentVersion = "v2"
	if e = s.SetSetting(ctx, SettingRegistration, RegistrationSettings{Legal: legal}); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateRegisteredUser(ctx, oauth, op); !errors.Is(e, ErrRegistrationLegal) {
		t.Fatal("stale terms accepted", e)
	}
	if e = s.SaveOAuthRegistrationConsent(ctx, "fresh", "example", legal.Version()); e != nil {
		t.Fatal(e)
	}
	op.OAuthState = "fresh"
	if e = s.CreateRegisteredUser(ctx, oauth, op); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateRegisteredUser(ctx, newUser("replay@example.com"), op); !errors.Is(e, ErrRegistrationLegal) {
		t.Fatal("flow replayed", e)
	}
}
func TestEmailCodesConcurrentSingleUseAndFailureLimit(t *testing.T) {
	s := New(testdb.Open(t))
	ctx := context.Background()
	code, e := s.NewCode(ctx, "one@example.com", "register", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { results <- s.CheckCode(ctx, "one@example.com", "register", code) })
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !errors.Is(e, ErrCodeInvalid) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal(successes)
	}
	if _, e = s.db.ExecContext(ctx, `INSERT INTO verification_codes(email,purpose,code_hash,expires_at,attempts,created_at)VALUES('wait@example.com','register','hash',?,0,?)`, time.Now().Add(time.Minute).Unix(), time.Now().Add(-30*time.Second).Unix()); e != nil {
		t.Fatal(e)
	}
	_, e = s.NewCode(ctx, "wait@example.com", "register", time.Minute)
	var cooldown *CodeCooldownError
	if !errors.As(e, &cooldown) || cooldown.RetryAfter < 29 || cooldown.RetryAfter > 30 {
		t.Fatal("not the remaining window", e)
	}
	code, e = s.NewCode(ctx, "two@example.com", "register", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for range 5 {
		if e = s.CheckCode(ctx, "two@example.com", "register", "wrong"); !errors.Is(e, ErrCodeInvalid) {
			t.Fatal(e)
		}
	}
	if e = s.CheckCode(ctx, "two@example.com", "register", code); !errors.Is(e, ErrCodeInvalid) {
		t.Fatal("challenge survived five failures", e)
	}
	_, e = s.NewCode(ctx, "expired@example.com", "register", -time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CheckCode(ctx, "expired@example.com", "register", "000000"); !errors.Is(e, ErrCodeInvalid) {
		t.Fatal(e)
	}
}
func TestRegistrationLegalValidation(t *testing.T) {
	for _, link := range []string{"javascript:alert(1)", "//example.com/terms", "https://user:pass@example.com/terms", "https://example.com/a\nb"} {
		l := &RegistrationLegal{TermsURL: link, DocumentVersion: "v1"}
		if l.Validate() == nil {
			t.Fatal(link)
		}
	}
}
