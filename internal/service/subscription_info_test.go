package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

func TestAccountInfoPlanBoundaries(t *testing.T) {
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	reset, expiry := at.Add(27*24*time.Hour), at.Add(90*24*time.Hour)
	old, after := at.Add(-time.Minute), expiry.Add(time.Hour)
	settings := store.AccountInfoSettings{Traffic: true, Reset: true, Expiry: true}
	subs := []*domain.Subscription{
		{ID: 1, PlanID: 1, Status: "active", QuotaBytes: 200 << 30, UsedUpBytes: 1 << 30, UsedDownBytes: 2 << 30, ResetAt: &reset, ExpiresAt: &expiry},
		// An exhausted plan still has useful accounting and reset information.
		{ID: 2, PlanID: 2, Status: "active", QuotaBytes: 1 << 30, UsedDownBytes: 1 << 30, ResetAt: &old},
		{ID: 3, PlanID: 3, Status: "active", ResetAt: &after, ExpiresAt: &expiry},
		{ID: 4, PlanID: 4, Status: "queued", QuotaBytes: 999},
		{ID: 5, PlanID: 5, Status: "active", ExpiresAt: &old},
		{ID: 6, PlanID: 6, Status: "active"},
	}
	labels := accountInfoLabels(subs, map[int64]string{1: "Basic", 2: "Exhausted", 3: "Unlimited", 4: "Queued", 5: "Expired", 6: "Lifetime"}, at, settings)
	joined := strings.Join(labels, "\n")
	for _, want := range []string{"Basic · Traffic: 3.00 GiB / 200.00 GiB", "2026-12-06 00:00 UTC (27 days left)", "Exhausted · Traffic reset: Pending", "Unlimited · Traffic: 0 B / Unlimited", "No reset before expiry", "Lifetime · Expires: Never", "Lifetime · Traffic reset: No scheduled reset"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "Queued") || strings.Contains(joined, "Expired") || len(labels) != 12 {
		t.Fatal(joined)
	}
	if got := accountInfoLabels(subs[:1], nil, at, store.AccountInfoSettings{Expiry: true}); len(got) != 1 || strings.Contains(got[0], "Traffic") {
		t.Fatal(got)
	}
	if got := accountInfoLabels(nil, nil, at, settings); !reflect.DeepEqual(got, []string{"No active subscription"}) {
		t.Fatal(got)
	}
}

func TestAccountInfoSelectorsPreserveRouting(t *testing.T) {
	line := subscription.Line{Name: "Example", Host: "node.example.com", Port: 443, Password: "example-password", Inbound: spec.Inbound{Protocol: spec.Shadowsocks, Cipher: "aes-128-gcm"}}
	for _, format := range []string{"clash", "stash"} {
		t.Run(format, func(t *testing.T) {
			body, err := subscription.Pick(format, "").Render([]subscription.Line{line}, subscription.Account{})
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			if err := yaml.Unmarshal(body, &before); err != nil {
				t.Fatal(err)
			}
			got, err := addAccountInfoGroups(body, []string{"Traffic: 3 GiB / 200 GiB", "Expires: Never"})
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(got, &after); err != nil {
				t.Fatal(err)
			}
			old := before["proxy-groups"].([]any)
			groups := after["proxy-groups"].([]any)
			if len(groups) != len(old)+3 || !reflect.DeepEqual(old, groups[:len(old)]) {
				t.Fatal(groups)
			}
			for _, raw := range groups[len(old):] {
				g := raw.(map[string]any)
				if g["type"] != "select" || g["use"] != nil || g["include-all"] != nil {
					t.Fatal(g)
				}
			}
			for _, raw := range groups[len(old)+1:] {
				if !reflect.DeepEqual(raw.(map[string]any)["proxies"], []any{"REJECT"}) {
					t.Fatal(raw)
				}
			}
			after["proxy-groups"] = groups[:len(old)]
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("routing or credentials changed: %s", got)
			}
		})
	}
}

func TestAccountInfoCustomTemplateCollisionsAndEscaping(t *testing.T) {
	const template = "proxies: []\nproxy-providers:\n  external:\n    type: file\n    path: ./proxies.yaml\nproxy-groups:\n  - name: Subscription info\n    type: load-balance\n    use: [external]\n    include-all: true\nrules:\n  - MATCH,Traffic\n"
	got, err := addAccountInfoGroups([]byte(template), []string{"Traffic", "Traffic", "Name: [x]\nnew-line"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	groups := doc["proxy-groups"].([]any)
	parent := groups[1].(map[string]any)
	if parent["name"] != "Subscription info (2)" || !reflect.DeepEqual(parent["proxies"], []any{"Traffic (2)", "Traffic (3)", "Name: [x]\nnew-line"}) {
		t.Fatal(string(got))
	}
	if !reflect.DeepEqual(doc["rules"], []any{"MATCH,Traffic"}) || len(doc["proxies"].([]any)) != 0 {
		t.Fatal(string(got))
	}
	if _, err := addAccountInfoGroups([]byte("proxies: []\n"), []string{"Usage"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"[unclosed", "[]", "proxy-groups: invalid"} {
		if _, err := addAccountInfoGroups([]byte(bad), []string{"Usage"}); err == nil {
			t.Fatal(bad)
		}
	}
}

func TestAccountInfoDisabledAndOtherFormatsUnchanged(t *testing.T) {
	s := &Subscription{} // No store: these paths must not even read the database.
	for _, format := range []string{"clash", "stash", "uri", "singbox", "surge", "loon", "qx", "wireguard", "egern", "surfboard"} {
		for _, enabled := range []bool{false, true} {
			if enabled && (format == "clash" || format == "stash") {
				continue
			}
			body := []byte("original bytes\n")
			got, err := s.WithAccountInfo(t.Context(), nil, time.Now(), format, body, store.AccountInfoSettings{Traffic: enabled})
			if err != nil || string(got) != string(body) {
				t.Fatal(format, err, string(got))
			}
		}
	}
}
