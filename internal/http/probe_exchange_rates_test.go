package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeFinanceCurrencyCompatibilityAndPublicRates(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	settingsPath := "/api/admin/settings/probe"
	readSettings := func(want string) {
		t.Helper()
		status, raw, _ := r.c.do("GET", settingsPath, nil, nil)
		if status != 200 {
			t.Fatal(status)
		}
		body := mustJSON[map[string]any](t, raw)
		if body["finance_currency"] != want {
			t.Fatalf("finance currency: %s", raw)
		}
	}
	save := func(body map[string]any, want int) {
		t.Helper()
		if status, raw, _ := r.c.do("PUT", settingsPath, body, nil); status != want {
			t.Fatalf("settings %d: %s", status, raw)
		}
	}
	readSettings("CNY")
	save(map[string]any{"enabled": true, "finance_currency": "eur", "public_sections": []string{"info"}}, 200)
	readSettings("EUR")
	save(map[string]any{"enabled": true}, 200)
	readSettings("EUR")
	save(map[string]any{"enabled": true, "finance_currency": nil}, 200)
	readSettings("EUR")
	for _, invalid := range []string{"", "ZZZ", "XXX", "XTS", "USD EUR", "US"} {
		save(map[string]any{"enabled": true, "finance_currency": invalid}, 400)
		readSettings("EUR")
	}
	anon := &client{t: t, srv: r.srv}
	getPublic := func(c *client) map[string]any {
		t.Helper()
		status, raw, _ := c.do("GET", "/api/probe", nil, nil)
		if status != 200 {
			t.Fatalf("public %d: %s", status, raw)
		}
		return mustJSON[map[string]any](t, raw)
	}
	missing := getPublic(anon)
	if missing["finance_currency"] != "EUR" || missing["exchange_rates"] != nil {
		t.Fatal("missing cache fabricated rates", missing)
	}
	at := time.Now().UTC()
	day := at.Format("2006-01-02")
	fx := &store.ProbeExchangeRates{Base: "USD", Rates: map[string]float64{"USD": 1, "CNY": 7.12, "EUR": 0.91}, Dates: map[string]string{"USD": day, "CNY": day, "EUR": day}, FetchedAt: at.Unix(), Source: store.ProbeExchangeRateSource}
	attempt, err := r.st.ClaimProbeExchangeRateRefresh(ctx, at)
	if err != nil || attempt == "" {
		t.Fatal("claim", attempt, err)
	}
	if updated, err := r.st.FinishProbeExchangeRateRefresh(ctx, attempt, fx, at); !updated || err != nil {
		t.Fatal("cache seed", updated, err)
	}
	// The service invalidates the cache after a refresh; the settings path also
	// invalidates it and lets this HTTP-only fixture exercise the next snapshot.
	save(map[string]any{"enabled": true, "finance_currency": "CNY", "public_sections": []string{"info"}}, 200)
	public := getPublic(anon)
	if public["finance_currency"] != "CNY" {
		t.Fatal("target missing", public)
	}
	quotes, ok := public["exchange_rates"].(map[string]any)
	if !ok || quotes["base"] != "USD" || quotes["source"] != "Frankfurter" || quotes["fetched_at"] != float64(at.Unix()) || quotes["stale"] != false {
		t.Fatal("public FX contract", quotes)
	}
	if quotes["rates"].(map[string]any)["CNY"] != 7.12 || quotes["dates"].(map[string]any)["EUR"] != day {
		t.Fatal("quote value/date changed", quotes)
	}
	// Visibility is governed by info, including staff views, independently of system.
	save(map[string]any{"enabled": true, "finance_currency": "JPY", "public_sections": []string{"cpu", "system"}}, 200)
	for _, client := range []*client{anon, r.c} {
		body := getPublic(client)
		if _, exposed := body["finance_currency"]; exposed {
			t.Fatal("hidden target exposed")
		}
		if _, exposed := body["exchange_rates"]; exposed {
			t.Fatal("hidden financial data exposed")
		}
	}
	save(map[string]any{"enabled": true, "public_sections": []string{"info"}, "visibility": "admins"}, 200)
	if status, _, _ := anon.do("GET", "/api/probe", nil, nil); status != 403 {
		t.Fatal("private rates accessible", status)
	}
	if body := getPublic(r.c); body["finance_currency"] != "JPY" || body["exchange_rates"] == nil {
		t.Fatal("authorized snapshot unavailable")
	}
	save(map[string]any{"enabled": false}, 200)
	if status, _, _ := r.c.do("GET", "/api/probe", nil, nil); status != 404 {
		t.Fatal("disabled probe exposed rates", status)
	}
}
