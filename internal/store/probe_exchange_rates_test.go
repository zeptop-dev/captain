package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestProbeExchangeRateClaimConcurrencyAndLastGood(t *testing.T) {
	s, _, at := monitorRig(t)
	ctx := context.Background()
	if err := s.SetSetting(ctx, SettingProbe, ProbeSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claims := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := s.ClaimProbeExchangeRateRefresh(ctx, at)
			claims <- claim
			errs <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	claim := ""
	for value := range claims {
		if value != "" {
			if claim != "" {
				t.Fatal("overlapping refresh claims")
			}
			claim = value
		}
	}
	if claim == "" {
		t.Fatal("missing refresh claim")
	}
	day := at.UTC().Format("2006-01-02")
	rates := &ProbeExchangeRates{Base: "USD", Rates: map[string]float64{"USD": 1, "EUR": 0.9}, Dates: map[string]string{"USD": day, "EUR": day}, FetchedAt: at.Unix(), Source: ProbeExchangeRateSource}
	if updated, err := s.FinishProbeExchangeRateRefresh(ctx, "obsolete", rates, at); updated || err != nil {
		t.Fatal("wrong claim installed", updated, err)
	}
	if updated, err := s.FinishProbeExchangeRateRefresh(ctx, claim, rates, at); !updated || err != nil {
		t.Fatal(updated, err)
	}
	next := at.Add(24 * time.Hour)
	claim, err := s.ClaimProbeExchangeRateRefresh(ctx, next)
	if err != nil || claim == "" {
		t.Fatal("daily retry", claim, err)
	}
	bad := *rates
	bad.Base = "EUR"
	bad.FetchedAt = next.Unix()
	if updated, err := s.FinishProbeExchangeRateRefresh(ctx, claim, &bad, next); updated || err != nil {
		t.Fatal("invalid cache replaced good data", updated, err)
	}
	got := s.ProbeExchangeRates(ctx, next)
	if got == nil || got.FetchedAt != at.Unix() || got.Rates["EUR"] != 0.9 || got.Dates["EUR"] != day {
		t.Fatal("last-good changed", got)
	}
	// Each read is independent; a caller cannot mutate the persisted cache.
	got.Rates["EUR"] = 100
	if again := s.ProbeExchangeRates(ctx, next); again == nil || again.Rates["EUR"] != 0.9 {
		t.Fatal("mutable cache shared with caller", again)
	}
	if err := s.SetSetting(ctx, SettingProbeExchangeRates, map[string]any{"snapshot": map[string]any{"rates": "malformed"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.ProbeExchangeRates(ctx, next); got != nil {
		t.Fatal("corrupt cache exposed", got)
	}
	if claim, err := s.ClaimProbeExchangeRateRefresh(ctx, next); err != nil || claim == "" {
		t.Fatal("corrupt cache could not recover", claim, err)
	}
}
