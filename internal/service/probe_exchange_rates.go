package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

const probeExchangeRateURL = "https://api.frankfurter.dev/v2/rates?base=USD"
const probeExchangeRateMaxBytes = 256 << 10

func newProbeExchangeRateClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 5 * time.Second
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("exchange rate redirects are not allowed")
		},
	}
}

var probeExchangeRateHTTP = newProbeExchangeRateClient()

func parseProbeExchangeRates(raw []byte, at time.Time) (*store.ProbeExchangeRates, error) {
	var rows []struct {
		Date  string  `json:"date"`
		Base  string  `json:"base"`
		Quote string  `json:"quote"`
		Rate  float64 `json:"rate"`
	}
	if len(raw) > probeExchangeRateMaxBytes || json.Unmarshal(raw, &rows) != nil || len(rows) == 0 || len(rows) > 512 {
		return nil, errors.New("invalid exchange rate response")
	}
	rates := &store.ProbeExchangeRates{Base: "USD", Rates: map[string]float64{}, Dates: map[string]string{}, FetchedAt: at.Unix(), Source: store.ProbeExchangeRateSource}
	for _, row := range rows {
		if row.Base != "USD" {
			return nil, errors.New("unexpected exchange rate base")
		}
		// The provider can know codes beyond the panel's bundled currency
		// vocabulary. Skip those without discarding supported quotes in the feed.
		if !store.ValidCurrency(row.Quote) {
			continue
		}
		if _, exists := rates.Rates[row.Quote]; exists {
			return nil, errors.New("duplicate exchange rate quote")
		}
		rates.Rates[row.Quote], rates.Dates[row.Quote] = row.Rate, row.Date
	}
	if !rates.Valid(at) {
		return nil, errors.New("invalid exchange rate observations")
	}
	return rates, nil
}

func fetchProbeExchangeRates(ctx context.Context, client *http.Client, at time.Time) (*store.ProbeExchangeRates, error) {
	// The URL and client policy are internal constants, never settings or request
	// parameters. No node names, currencies in use or operator addresses are sent.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeExchangeRateURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange rate source returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, probeExchangeRateMaxBytes+1))
	if err != nil {
		return nil, err
	}
	return parseProbeExchangeRates(raw, at)
}

// RefreshExchangeRates is called by the background runner, never a public or
// admin handler. Claims and completion use short database transactions; no
// Accounts lock or database transaction is held over the network request.
func (p *Probe) RefreshExchangeRates(ctx context.Context, at time.Time) error {
	attempt, err := p.Store.ClaimProbeExchangeRateRefresh(ctx, at)
	if err != nil || attempt == "" {
		return err
	}
	client := p.exchangeRateClient
	if client == nil {
		client = probeExchangeRateHTTP
	}
	rates, fetchErr := fetchProbeExchangeRates(ctx, client, at)
	updated, saveErr := p.Store.FinishProbeExchangeRateRefresh(ctx, attempt, rates, at)
	if updated {
		p.Invalidate()
	}
	return errors.Join(fetchErr, saveErr)
}
