package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/mail"
)

func TestMailTemplatesHTTP(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	var ms mail.Settings
	ms.Provider = "smtp"
	ms.SMTP.Host = "smtp.example.com"
	ms.SMTP.Password = "example-secret"
	ms.FromAddress = "noreply@example.com"
	if err := r.st.SetSetting(ctx, mail.SettingKey, ms); err != nil {
		t.Fatal(err)
	}
	d := mail.Design{Name: "Example Brand", Templates: map[string]map[string]mail.Template{"zh-CN": {"register": {Subject: "{{site}} registration", Body: "Hello {{site}}"}}}}
	const path = "/api/admin/settings/mail/templates"
	if code, b, _ := r.c.do("PUT", path, d, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	var stored mail.Settings
	r.st.GetSetting(ctx, mail.SettingKey, &stored)
	if stored.SMTP.Password != "example-secret" {
		t.Fatal("SMTP settings overwritten")
	}
	var sent []mail.Message
	original := mail.SendFunc
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error { sent = append(sent, m); return nil }
	defer func() { mail.SendFunc = original }()
	sample := map[string]any{"kind": "register", "language": "zh-CN", "design": d, "To": "sample@example.com"}
	code, b, _ := r.c.do("POST", "/api/admin/settings/mail/preview", sample, nil)
	if code != 200 || !strings.Contains(string(b), "Example Brand registration") || !strings.Contains(string(b), "123456") || len(sent) != 0 {
		t.Fatal(code, string(b), sent)
	}
	if code, b, _ := r.c.do("POST", "/api/admin/settings/mail/test", sample, nil); code != 200 || len(sent) != 1 || sent[0].To != "sample@example.com" || !strings.HasPrefix(sent[0].Subject, "[TEST]") {
		t.Fatal(code, string(b), sent)
	}
	var codes, notifications int
	r.st.DB().QueryRow(`SELECT count(*) FROM verification_codes`).Scan(&codes)
	r.st.DB().QueryRow(`SELECT count(*) FROM notifications`).Scan(&notifications)
	if codes != 0 || notifications != 0 {
		t.Fatal("sample changed customer records", codes, notifications)
	}
	// Real reset delivery also uses the saved design, not merely the sample path.
	d.Templates["zh-CN"]["reset"] = mail.Template{Subject: "{{site}} reset", Body: "Reset for {{site}}"}
	if code, b, _ := r.c.do("PUT", path, d, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	ms.Language = "zh-CN"
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/mail", ms, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	id, _, _ := profileSetup(t, r)
	user, err := r.st.UserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if code, b, _ := r.c.do("POST", "/api/portal/verify/send", map[string]string{"Email": user.Email, "Purpose": "reset"}, nil); code != 200 || len(sent) != 2 || sent[1].Subject != "Example Brand reset" {
		t.Fatal(code, string(b), sent)
	}
	// Reminder placeholders come from this subscription row.
	if _, err := r.st.DB().Exec(`UPDATE subscriptions SET used_up_bytes=?,used_down_bytes=?,quota_bytes=?,expires_at=?,reset_at=? WHERE user_id=?`, 40<<30, 50<<30, 100<<30, time.Now().Add(time.Hour).Unix(), time.Now().Add(30*time.Minute).Unix(), id); err != nil {
		t.Fatal(err)
	}
	for _, expiry := range []bool{false, true} {
		rows, err := r.st.HighTrafficSubscriptions(ctx, 90)
		if expiry {
			rows, err = r.st.ExpiringSubscriptions(ctx, time.Now(), 3*24*time.Hour)
		}
		if err != nil || len(rows) != 1 || rows[0].UsedBytes != 90<<30 || rows[0].QuotaBytes != 100<<30 || rows[0].PlanName == "" || rows[0].ResetAt == 0 {
			t.Fatal(rows, err)
		}
	}
	d.Color = "invalid"
	if code, _, _ := r.c.do("PUT", path, d, nil); code != 400 {
		t.Fatal(code)
	}
	var saved mail.Design
	r.st.GetSetting(ctx, mail.DesignKey, &saved)
	if saved.Color != "" {
		t.Fatal("bad draft replaced stored template")
	}
	for _, role := range []string{"operator", "support"} {
		staff, _ := admin.NewUser(role+"-mail@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, staff); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, nil)
		for _, endpoint := range []string{path, "/api/admin/settings/mail/preview"} {
			method := "GET"
			var body any
			if strings.HasSuffix(endpoint, "preview") {
				method = "POST"
				body = sample
			}
			if code, _, _ := c.do(method, endpoint, body, nil); code != 403 {
				t.Fatal(role, endpoint, code)
			}
		}
	}
}
