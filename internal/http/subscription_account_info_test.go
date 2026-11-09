package http

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionAccountInfoHTTPIsolationAndPreview(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id, path, profile := profileSetup(t, r)
	otherID, _ := r.user("other-info@example.com")
	other, err := r.st.UserByID(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if _, err := r.st.DB().Exec(`UPDATE subscriptions SET quota_bytes=?,used_up_bytes=?,used_down_bytes=?,reset_at=?,expires_at=? WHERE user_id=?`, 200<<30, 1<<30, 2<<30, at.Add(27*24*time.Hour).Unix(), at.Add(90*24*time.Hour).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if err := r.st.AssignSubscriptionProfile(ctx, id, &profile.ID); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, format := range []string{"clash", "stash", "singbox", "uri", "surge", "loon", "qx", "egern", "surfboard"} {
		code, b, _ := r.c.do("GET", path+"?client="+format, nil, nil)
		if code != 200 {
			t.Fatal(format, code, string(b))
		}
		before[format] = b
	}
	const settingsPath = "/api/admin/settings/subscription"
	if code, b, _ := r.c.do("PUT", settingsPath, map[string]any{"account_info": store.AccountInfoSettings{Traffic: true, Reset: true, Expiry: true}}, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	// Another independently saved section must preserve the display policy.
	if code, b, _ := r.c.do("PUT", settingsPath, map[string]any{"single_plan": true}, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	for format, original := range before {
		code, b, headers := r.c.do("GET", path+"?client="+format, nil, nil)
		if code != 200 || !strings.Contains(headers.Get("Subscription-Userinfo"), "upload=1073741824; download=2147483648; total=214748364800") {
			t.Fatal(format, code, string(b), headers)
		}
		if format != "clash" && format != "stash" {
			if string(original) != string(b) {
				t.Fatalf("%s changed", format)
			}
			continue
		}
		if !strings.Contains(string(b), "3.00 GiB / 200.00 GiB") || !strings.Contains(string(b), "Traffic reset:") || !strings.Contains(string(b), "Expires:") {
			t.Fatal(format, string(b))
		}
		var oldDoc, newDoc map[string]any
		if err := yaml.Unmarshal(original, &oldDoc); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(b, &newDoc); err != nil {
			t.Fatal(err)
		}
		oldGroups, _ := oldDoc["proxy-groups"].([]any)
		groups := newDoc["proxy-groups"].([]any)
		if len(groups) != len(oldGroups)+4 {
			t.Fatal(format, groups)
		}
		if oldDoc["proxy-groups"] == nil {
			delete(newDoc, "proxy-groups")
		} else {
			newDoc["proxy-groups"] = groups[:len(oldGroups)]
		}
		if !reflect.DeepEqual(oldDoc, newDoc) {
			t.Fatalf("%s changed real config: %s", format, b)
		}
	}
	code, b, _ := r.c.do("GET", "/sub/"+other.SubToken+"?client=clash", nil, nil)
	if code != 200 || strings.Contains(string(b), "200.00 GiB") || strings.Contains(string(b), "profile@example.com") {
		t.Fatal("another customer saw account information", code, string(b))
	}
	// Override can disable the global display, and preview uses the same path.
	profile.Settings.AccountInfo = &store.AccountInfoSettings{}
	if err := r.st.SaveSubscriptionProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	code, b, _ = r.c.do("GET", path+"?client=clash", nil, nil)
	if code != 200 || string(b) != string(before["clash"]) {
		t.Fatal("profile disable failed", code, string(b))
	}
	profile.Settings.AccountInfo = &store.AccountInfoSettings{Traffic: true}
	code, b, _ = r.c.do("POST", "/api/admin/settings/sub-profiles/preview", map[string]any{"profile": profile, "user_id": id, "format": "clash"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	preview := mustJSON[struct {
		Body string `json:"body"`
	}](t, b)
	if !strings.Contains(preview.Body, "3.00 GiB / 200.00 GiB") || strings.Contains(preview.Body, "Traffic reset:") {
		t.Fatal(preview.Body)
	}
	// Account information never bypasses HWID refusal or disabled-account denial.
	yes, one := true, 1
	profile.Settings.HWID = &store.ProfileHWID{Enabled: &yes, DeviceLimit: &one}
	if err := r.st.SaveSubscriptionProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	r.c.do("GET", path+"?client=clash", nil, map[string]string{"x-hwid": "info-device-a"})
	code, b, head := r.c.do("GET", path+"?client=clash", nil, map[string]string{"x-hwid": "info-device-b"})
	if code != 200 || head.Get("x-hwid-max-devices-reached") != "true" || strings.Contains(string(b), "Subscription info") || strings.Contains(string(b), "200.00 GiB") {
		t.Fatal(code, string(b), head)
	}
	if _, err := r.st.DB().Exec(`UPDATE users SET status='banned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.c.do("GET", path+"?client=clash", nil, nil); code != 403 {
		t.Fatal(code)
	}
	// Same existing settings endpoint remains administrator-only.
	for _, role := range []string{"operator", "support"} {
		staff, _ := admin.NewUser(role+"-info@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, staff); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, nil)
		if code, _, _ := c.do("PUT", settingsPath, map[string]any{"account_info": store.AccountInfoSettings{}}, nil); code != 403 {
			t.Fatal(role, code)
		}
	}
	var settings service.SubscriptionSettings
	if err := r.st.GetSetting(ctx, service.SettingSubscription, &settings); err != nil || !settings.AccountInfo.Traffic || !settings.AccountInfo.Reset || !settings.AccountInfo.Expiry {
		t.Fatal(settings, err)
	}
}
