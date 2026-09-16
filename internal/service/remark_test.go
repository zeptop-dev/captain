package service

import (
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestRemarkVars(t *testing.T) {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	exp := at.Add(10*24*time.Hour + time.Hour)
	u := &domain.User{Email: "amy@example.com", Status: "active"}
	usable := []*domain.Subscription{{PlanID: 1}}
	v := NewRemarkVars(u, usable, map[int64]string{1: "Pro"}, subscription.Account{Upload: 1 << 30, Download: 3 << 30, Total: 10 << 30, Expire: exp.Unix()}, at)
	cases := map[string]string{
		"plain": "plain",
		"🇭🇰 HK {{DAYS_LEFT}}d {{TRAFFIC_LEFT}}":                "🇭🇰 HK 10d 6.0 GB",
		"{{TRAFFIC_USED}}/{{TRAFFIC_LIMIT}} {{USED_PERCENT}}%": "4.0 GB/10.0 GB 40%",
		"{{STATUS}} {{PLAN}} {{USERNAME}} {{EMAIL}}":           "active Pro amy amy@example.com",
		"{{STATUS:ACTIVE=✅|EXPIRED=😓}} ok":                     "✅ ok",
		"{{unknown}} {{":                                       "{{unknown}} {{",
		"{{ days_left }}":                                      "10",
	}
	for in, want := range cases {
		if got := v.Expand(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if got := v.Expand("{{EXPIRE_DATE}}"); got != exp.Local().Format("2006-01-02") {
		t.Errorf("EXPIRE_DATE = %q", got)
	}
	// Unlimited and no expiry.
	v2 := NewRemarkVars(u, usable, nil, subscription.Account{}, at)
	if got := v2.Expand("{{DAYS_LEFT}} {{TRAFFIC_LEFT}} {{TRAFFIC_LIMIT}} {{USED_PERCENT}}"); got != "∞ ∞ ∞ 0" {
		t.Errorf("unlimited: %q", got)
	}
	// Status derivation.
	past := at.Add(-time.Hour)
	v3 := NewRemarkVars(u, nil, nil, subscription.Account{Expire: past.Unix()}, at)
	v4 := NewRemarkVars(u, nil, nil, subscription.Account{Total: 10, Upload: 10}, at)
	v5 := NewRemarkVars(&domain.User{Status: "banned"}, nil, nil, subscription.Account{}, at)
	if v3.Status != "expired" || v4.Status != "limited" || v5.Status != "disabled" {
		t.Errorf("status: %s %s %s", v3.Status, v4.Status, v5.Status)
	}
	if got := v3.Expand("{{DAYS_LEFT}}"); got != "0" {
		t.Errorf("past DAYS_LEFT = %q", got)
	}
	// Info lines render in every format without error.
	for _, f := range []string{"clash", "singbox", "surge", "loon", "qx", "uri", "egern", "surfboard", "stash"} {
		if _, err := subscription.Pick(f, "").RenderWith([]subscription.Line{InfoLine(v.Expand("剩余 {{TRAFFIC_LEFT}}"), 0)}, subscription.Account{}, ""); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
