package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/store"
)

type probeFXTransport func(*http.Request) (*http.Response, error)

func (f probeFXTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const probeFXFixture = `[{"date":"2026-10-25","base":"USD","quote":"USD","rate":1},{"date":"2026-10-25","base":"USD","quote":"CNY","rate":7.12},{"date":"2026-10-23","base":"USD","quote":"EUR","rate":0.91},{"date":"2026-10-24","base":"USD","quote":"ZZZ","rate":7.11},{"date":"2026-10-24","base":"USD","quote":"XAU","rate":0.00025},{"date":"2026-10-24","base":"USD","quote":"IRR","rate":1839564}]`

func probeFXResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func probeFXRig(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "fx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return store.New(conn), time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
}

func TestProbeExchangeRatesParseAndHTTPPolicy(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	rates, err := parseProbeExchangeRates([]byte(probeFXFixture), at)
	if err != nil || len(rates.Rates) != 5 || rates.Rates["USD"] != 1 || rates.Rates["CNY"] != 7.12 || rates.Dates["EUR"] != "2026-10-23" || rates.Dates["CNY"] != "2026-10-25" {
		t.Fatalf("actual v2 schema/mixed dates/ISO subset: %+v %v", rates, err)
	}
	for _, raw := range []string{
		"", "[]", `{}`, `{"rates":{"USD":1}}`,
		strings.ReplaceAll(probeFXFixture, `"base":"USD"`, `"base":"EUR"`),
		strings.Replace(probeFXFixture, `"rate":1}`, `"rate":0}`, 1),
		strings.Replace(probeFXFixture, `"rate":1}`, `"rate":1.01}`, 1),
		strings.Replace(probeFXFixture, `"rate":7.12`, `"rate":-1`, 1),
		strings.Replace(probeFXFixture, `"rate":7.12`, `"rate":1e999`, 1),
		strings.Replace(probeFXFixture, `"2026-10-25"`, `"2026-11-01"`, 1),
		strings.Replace(probeFXFixture, `"2026-10-25"`, `"2026-02-30"`, 1),
		strings.Replace(probeFXFixture, `"quote":"EUR"`, `"quote":"CNY"`, 1),
		probeFXFixture + strings.Repeat(" ", probeExchangeRateMaxBytes),
	} {
		if got, err := parseProbeExchangeRates([]byte(raw), at); err == nil || got != nil {
			t.Fatalf("accepted malformed feed: %.100s", raw)
		}
	}
	client := newProbeExchangeRateClient()
	if client.Timeout != 10*time.Second || client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("unsafe fetch policy")
	}
	calls := 0
	client.Transport = probeFXTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != probeExchangeRateURL || r.Method != "GET" || r.Header.Get("Accept") != "application/json" {
			t.Fatal("unexpected outbound request", r.URL)
		}
		response := probeFXResponse("")
		response.StatusCode = 302
		response.Header.Set("Location", "https://example.com/redirect-target")
		return response, nil
	})
	if _, err := fetchProbeExchangeRates(context.Background(), client, at); err == nil || calls != 1 {
		t.Fatal("followed redirect", calls, err)
	}
	client.Transport = probeFXTransport(func(*http.Request) (*http.Response, error) {
		return probeFXResponse(probeFXFixture + strings.Repeat(" ", probeExchangeRateMaxBytes)), nil
	})
	if _, err := fetchProbeExchangeRates(context.Background(), client, at); err == nil {
		t.Fatal("unbounded body accepted")
	}
}

func TestProbeExchangeRatesRefreshPersistenceRetryAndGating(t *testing.T) {
	st, at := probeFXRig(t)
	ctx := context.Background()
	calls := 0
	failing := false
	client := newProbeExchangeRateClient()
	client.Transport = probeFXTransport(func(_ *http.Request) (*http.Response, error) {
		calls++
		if !st.Accounts.TryLock() {
			t.Fatal("network held Accounts lock")
		}
		st.Accounts.Unlock()
		queryCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var count int
		if err := st.DB().QueryRowContext(queryCtx, `SELECT COUNT(*) FROM settings`).Scan(&count); err != nil {
			t.Fatal("network held database transaction", err)
		}
		if failing {
			return nil, errors.New("fixture source outage")
		}
		return probeFXResponse(probeFXFixture), nil
	})
	p := &Probe{Store: st, exchangeRateClient: client}
	if err := p.RefreshExchangeRates(ctx, at); err != nil || calls != 0 {
		t.Fatal("disabled probe fetched", calls, err)
	}
	settings := store.ProbeSettings{Enabled: true, PublicSections: &[]string{"cpu"}}
	if err := st.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	if err := p.RefreshExchangeRates(ctx, at); err != nil || calls != 0 {
		t.Fatal("hidden finance fetched", calls, err)
	}
	settings.PublicSections = &[]string{"info"}
	if err := st.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	before := p.Gen()
	if err := p.RefreshExchangeRates(ctx, at); err != nil || calls != 1 || p.Gen() == before {
		t.Fatal("first refresh/cache invalidation", calls, err)
	}
	cache := st.ProbeExchangeRates(ctx, at)
	if cache == nil || cache.FetchedAt != at.Unix() || cache.Stale {
		t.Fatal("successful cache missing", cache)
	}
	// The next process sees the successful cache and persisted daily schedule.
	p = &Probe{Store: store.New(st.DB()), exchangeRateClient: client}
	if err := p.RefreshExchangeRates(ctx, at.Add(23*time.Hour)); err != nil || calls != 1 {
		t.Fatal("restart fetched early", calls, err)
	}
	failing = true
	if err := p.RefreshExchangeRates(ctx, at.Add(24*time.Hour)); err == nil || calls != 2 {
		t.Fatal("expected scheduled failure", calls, err)
	}
	p = &Probe{Store: store.New(st.DB()), exchangeRateClient: client}
	if err := p.RefreshExchangeRates(ctx, at.Add(24*time.Hour+14*time.Minute)); err != nil || calls != 2 {
		t.Fatal("failure retry ignored persisted delay", calls, err)
	}
	for _, elapsed := range []time.Duration{24*time.Hour + 15*time.Minute, 25*time.Hour + 15*time.Minute, 31*time.Hour + 15*time.Minute} {
		if err := p.RefreshExchangeRates(ctx, at.Add(elapsed)); err == nil {
			t.Fatal("outage unexpectedly succeeded")
		}
	}
	if calls != 5 {
		t.Fatal("unexpected bounded retry count", calls)
	}
	if err := p.RefreshExchangeRates(ctx, at.Add(50*time.Hour)); err != nil || calls != 5 {
		t.Fatal("retry flood", calls, err)
	}
	cache = st.ProbeExchangeRates(ctx, at.Add(50*time.Hour))
	if cache == nil || !cache.Stale || cache.FetchedAt != at.Unix() || cache.Dates["CNY"] != "2026-10-25" {
		t.Fatal("failure changed last-good observations", cache)
	}
	failing = false
	if err := p.RefreshExchangeRates(ctx, at.Add(56*time.Hour)); err != nil || calls != 6 {
		t.Fatal("recovery failed", calls, err)
	}
	cache = st.ProbeExchangeRates(ctx, at.Add(56*time.Hour))
	if cache == nil || cache.Stale || cache.FetchedAt != at.Add(56*time.Hour).Unix() {
		t.Fatal("recovery did not replace cache", cache)
	}
}

func TestProbeExchangeRatesInflightPolicyAndReset(t *testing.T) {
	st, at := probeFXRig(t)
	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	client := newProbeExchangeRateClient()
	client.Transport = probeFXTransport(func(*http.Request) (*http.Response, error) {
		if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{}); err != nil {
			t.Fatal(err)
		}
		return probeFXResponse(probeFXFixture), nil
	})
	p := &Probe{Store: st, exchangeRateClient: client}
	if err := p.RefreshExchangeRates(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got := st.ProbeExchangeRates(ctx, at); got != nil {
		t.Fatal("late disabled response installed", got)
	}
	if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	client.Transport = probeFXTransport(func(*http.Request) (*http.Response, error) {
		// Simulate reset's removal of persisted settings followed by re-enabling.
		if _, err := st.DB().ExecContext(ctx, `DELETE FROM settings`); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSetting(ctx, store.SettingProbe, store.ProbeSettings{Enabled: true}); err != nil {
			t.Fatal(err)
		}
		return probeFXResponse(probeFXFixture), nil
	})
	if err := p.RefreshExchangeRates(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got := st.ProbeExchangeRates(ctx, at); got != nil {
		t.Fatal("pre-reset response repopulated new site", got)
	}
}
