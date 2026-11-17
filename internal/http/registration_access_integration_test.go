package http

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestRegisteredIdentitySurvivesPlanGrantMigrationAndRenumbering(t *testing.T) {
	ctx := context.Background()
	oldSend := mail.SendFunc
	var last mail.Message
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error { last = m; return nil }
	defer func() { mail.SendFunc = oldSend }()
	legal := &store.RegistrationLegal{TermsURL: "https://example.com/terms", DocumentVersion: "v1"}
	r := newPortalRig(t, true, func(s *store.Store) {
		ms := mail.Settings{Provider: "smtp", FromAddress: "noreply@example.com", VerifyRegistration: true}
		ms.SMTP.Host = "smtp.example.com"
		if e := s.SetSetting(ctx, mail.SettingKey, ms); e != nil {
			t.Fatal(e)
		}
		if e := s.SetSetting(ctx, store.SettingRegistration, store.RegistrationSettings{Legal: legal}); e != nil {
			t.Fatal(e)
		}
	})
	customer := &client{t: t, srv: r.srv}
	code, b, _ := customer.do("POST", "/api/portal/verify/send", map[string]string{"Email": "combined@example.com", "Purpose": "register"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, b, _ = customer.do("POST", "/api/portal/register", map[string]any{"Email": "combined@example.com", "Password": "password123", "Code": extractCode(last.Subject), "LegalAccepted": true, "LegalVersion": legal.Version()}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	u, e := r.st.UserByEmail(ctx, "combined@example.com")
	if e != nil {
		t.Fatal(e)
	}
	originalID, identity, uuid, token := u.ID, u.AgentID, u.UUID, u.SubToken
	group, e := r.st.CreateGroup(ctx, "Combined permission")
	if e != nil {
		t.Fatal(e)
	}
	plan := &domain.Plan{Name: "Combined plan", GroupID: &group, PeriodDays: 30, QuotaBytes: 1024, Enabled: true}
	if e = r.st.CreatePlan(ctx, plan); e != nil {
		t.Fatal(e)
	}
	if _, e = r.st.GrantSubscriptionMode(ctx, u.ID, plan, time.Now(), store.GrantStack); e != nil {
		t.Fatal(e)
	}
	if e = r.st.UpdateUser(ctx, u.ID, "active", &group, ""); e != nil {
		t.Fatal(e)
	}
	path := "/api/admin/users/" + itoa(u.ID)
	code, b, _ = r.admin.do("PATCH", path, map[string]any{"Status": "active", "Password": ""}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	u = r.reload(u.ID)
	if u.GroupID == nil || *u.GroupID != group {
		t.Fatal("profile edit lost authorization", u)
	}
	code, b, _ = r.admin.do("GET", path+"/legacy-grant", nil, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	preview := mustJSON[store.LegacyUserGrant](t, b)
	if !preview.CanMigrate {
		t.Fatal(preview)
	}
	// Numeric changes must carry both the new email/consent references and the
	// old authorization, without changing immutable credentials or the session.
	code, b, _ = r.admin.do("PUT", path+"/id", map[string]any{"id": 999}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	u = r.reload(999)
	if u.AgentID != identity || u.UUID != uuid || u.SubToken != token {
		t.Fatal("identity changed", u)
	}
	before, e := r.st.Subscriptions(ctx, 999)
	if e != nil {
		t.Fatal(e)
	}
	code, b, _ = r.admin.do("POST", "/api/admin/users/999/legacy-grant/migrate", map[string]any{"confirm": true, "revision": preview.Revision}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	u = r.reload(999)
	if u.GroupID != nil || u.AgentID != identity || u.UUID != uuid || u.SubToken != token {
		t.Fatal("migration changed identity", u)
	}
	after, e := r.st.Subscriptions(ctx, 999)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration changed billing", before, after, e)
	}
	verification, e := r.st.EmailVerification(ctx, 999)
	if e != nil || !verification.Verified || verification.Source != "email_code" {
		t.Fatal("email proof lost", verification, e)
	}
	var receipts int
	if e = r.db.QueryRow(`SELECT count(*) FROM registration_receipts WHERE user_id=? AND document_version='v1'`, 999).Scan(&receipts); e != nil || receipts != 1 {
		t.Fatal("receipt lost", receipts, e)
	}
	if e = r.db.QueryRow(`SELECT count(*) FROM registration_receipts WHERE user_id=?`, originalID).Scan(&receipts); e != nil || receipts != 0 {
		t.Fatal("stale owner reference", receipts, e)
	}
	code, b, _ = customer.do("GET", "/api/portal/me", nil, nil)
	if code != 200 || !strings.Contains(string(b), `"id":999`) || !strings.Contains(string(b), `"verified":true`) {
		t.Fatal("session/proof lost", code, string(b))
	}
}
