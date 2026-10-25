package http

import (
	"context"
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestSubDesignResourcesDeliveredWithoutAccountTokens(t *testing.T) {
	r := newRigAt(t, "https://panel.example.com")
	_, path, _ := profileSetup(t, r)
	design, _ := subdesign.Preset("basic")
	design.Enhancements = subdesign.Enhancements{Claude: true, WebRTC: true, QUIC: true}
	templates := store.SubTemplates{}
	for _, format := range []string{"qx", "loon"} {
		body, err := design.Render(format)
		if err != nil {
			t.Fatal(err)
		}
		templates[format] = body
	}
	if err := r.st.SetSetting(context.Background(), store.SettingSubTemplates, templates); err != nil {
		t.Fatal(err)
	}
	visitor := &client{t: t, srv: r.srv}
	resourceRE := regexp.MustCompile(`https://panel\.example\.com/sub-resources/[^,\s]+`)
	for _, format := range []string{"qx", "loon"} {
		code, body, _ := visitor.do("GET", path+"?client="+format, nil, map[string]string{"X-Forwarded-Host": "attacker.example.com"})
		if format == "qx" && code == 200 {
			var err error
			body, err = base64.StdEncoding.DecodeString(string(body))
			if err != nil {
				t.Fatal(err)
			}
		}
		if code != 200 || strings.Contains(string(body), "{{") || strings.Contains(string(body), "attacker.example.com") {
			t.Fatalf("%s delivery: %d %s", format, code, body)
		}
		resources := resourceRE.FindAllString(string(body), -1)
		if len(resources) == 0 {
			t.Fatalf("%s lacks the resource preserving its trailing GEOIP rule", format)
		}
		for _, raw := range resources {
			u, err := url.Parse(raw)
			if err != nil || strings.Contains(raw, strings.TrimPrefix(path, "/sub/")) {
				t.Fatal("managed resource contains a subscription token")
			}
			status, resource, headers := visitor.do("GET", u.RequestURI(), nil, nil)
			if status != 200 || len(resource) == 0 || headers.Get("X-Content-Type-Options") != "nosniff" || strings.Contains(string(resource), "{{") {
				t.Fatalf("public resource %s: %d", u.Path, status)
			}
		}
	}
	if code, _, _ := visitor.do("GET", "/sub/invalid?client=qx", nil, nil); code != 404 {
		t.Fatal("public resources must not make invalid subscription tokens usable")
	}
}
