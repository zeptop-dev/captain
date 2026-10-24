package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"golang.org/x/text/currency"
)

// ValidCurrency uses the same ISO-4217 vocabulary as recorded server prices.
// Syntactically valid codes unsupported by the provider may still be selected.
func ValidCurrency(code string) bool {
	unit, err := currency.ParseISO(code)
	return err == nil && unit.String() == code && code != "XXX" && code != "XTS"
}

const SettingProbeExchangeRates = "probe_exchange_rates"
const ProbeExchangeRateSource = "Frankfurter"

// ProbeExchangeRates quotes one unit of Base in each currency. Dates belong to
// individual quotes; neither a successful fetch nor another quote refreshes them.
type ProbeExchangeRates struct {
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	Dates     map[string]string  `json:"dates"`
	FetchedAt int64              `json:"fetched_at"`
	Stale     bool               `json:"stale"`
	Source    string             `json:"source"`
}

func (r *ProbeExchangeRates) Valid(at time.Time) bool {
	if r == nil || r.Base != "USD" || r.Source != ProbeExchangeRateSource || r.FetchedAt <= 0 || r.FetchedAt > at.Add(5*time.Minute).Unix() || len(r.Rates) < 2 || len(r.Rates) > 512 || len(r.Dates) != len(r.Rates) || r.Rates["USD"] != 1 {
		return false
	}
	for quote, rate := range r.Rates {
		if !ValidCurrency(quote) || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return false
		}
		date, err := time.Parse("2006-01-02", r.Dates[quote])
		// Official sources can carry the next local business date. Preserve that
		// quote date while rejecting clearly impossible future observations.
		if err != nil || date.Year() < 1 || date.After(at.UTC().Truncate(24*time.Hour).AddDate(0, 0, 2)) {
			return false
		}
	}
	return true
}

type probeExchangeRateState struct {
	Snapshot    *ProbeExchangeRates `json:"snapshot,omitempty"`
	Attempt     string              `json:"attempt,omitempty"`
	Failures    int                 `json:"failures"`
	NextAttempt int64               `json:"next_attempt"`
}

func readProbeExchangeRateState(ctx context.Context, tx *sql.Tx) (probeExchangeRateState, error) {
	var state probeExchangeRateState
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbeExchangeRates).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	// This is a cache, not a ledger. A malformed cache can be fetched afresh.
	if json.Unmarshal([]byte(raw), &state) != nil {
		return probeExchangeRateState{}, nil
	}
	return state, nil
}

func saveProbeExchangeRateState(ctx context.Context, tx *sql.Tx, state probeExchangeRateState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, SettingProbeExchangeRates, string(raw))
	return err
}

func probeFinanceEnabled(ctx context.Context, tx *sql.Tx) (bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbe).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var cfg ProbeSettings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return false, err
	}
	return cfg.Enabled && cfg.PublicShows("info"), nil
}

// Claim persists retry timing before networking. Restarts and concurrent ticks
// cannot turn a provider failure into an unbounded request loop.
func (s *Store) ClaimProbeExchangeRateRefresh(ctx context.Context, at time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	enabled, err := probeFinanceEnabled(ctx, tx)
	if err != nil || !enabled {
		return "", err
	}
	state, err := readProbeExchangeRateState(ctx, tx)
	if err != nil {
		return "", err
	}
	if at.Unix() < state.NextAttempt {
		return "", nil
	}
	state.Attempt = rand.Text()
	state.Failures = min(max(0, state.Failures)+1, 4)
	delays := [...]time.Duration{15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}
	state.NextAttempt = at.Add(delays[state.Failures-1]).Unix()
	if err := saveProbeExchangeRateState(ctx, tx, state); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return state.Attempt, nil
}

// Finish rechecks current policy and the persisted claim. A site reset or a
// replacement attempt makes an in-flight response obsolete, even if finance is
// enabled again. Last-good observations survive failed attempts unchanged.
func (s *Store) FinishProbeExchangeRateRefresh(ctx context.Context, attempt string, rates *ProbeExchangeRates, at time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	state, err := readProbeExchangeRateState(ctx, tx)
	if err != nil {
		return false, err
	}
	if attempt == "" || state.Attempt != attempt {
		return false, nil
	}
	enabled, err := probeFinanceEnabled(ctx, tx)
	if err != nil {
		return false, err
	}
	state.Attempt = ""
	updated := enabled && rates.Valid(at)
	if updated {
		rates.Stale = false
		state.Snapshot = rates
		state.Failures = 0
		state.NextAttempt = at.Add(24 * time.Hour).Unix()
	} else if !enabled {
		state.NextAttempt = 0 // Re-enabling can refresh promptly.
	}
	if err := saveProbeExchangeRateState(ctx, tx, state); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return updated, nil
}

// ProbeExchangeRates reads only persisted observations; public requests never
// perform provider calls. A cache with missing/invalid data stays unavailable.
func (s *Store) ProbeExchangeRates(ctx context.Context, at time.Time) *ProbeExchangeRates {
	var state probeExchangeRateState
	if s.GetSetting(ctx, SettingProbeExchangeRates, &state) != nil || !state.Snapshot.Valid(at) {
		return nil
	}
	state.Snapshot.Stale = at.Unix()-state.Snapshot.FetchedAt >= int64((48*time.Hour)/time.Second)
	return state.Snapshot
}
