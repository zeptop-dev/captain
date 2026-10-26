package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// Geolocation is panel-side and uses only the observed public IP. It never
// connects to that IP, accepts a user-supplied URL, or sends node credentials.
func lookupLocation(ctx context.Context, ip string) (*domain.GeoLocation, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	return lookupLocationWithClient(ctx, ip, client)
}

// Country-only wrappers retain the existing test/integration seam.
func lookupCountry(ctx context.Context, ip string) (string, error) {
	location, err := lookupLocation(ctx, ip)
	if location == nil {
		return "", err
	}
	return location.Country, err
}
func lookupCountryWithClient(ctx context.Context, ip string, client *http.Client) (string, error) {
	location, err := lookupLocationWithClient(ctx, ip, client)
	if location == nil {
		return "", err
	}
	return location.Country, err
}
func lookupLocationWithClient(ctx context.Context, ip string, client *http.Client) (*domain.GeoLocation, error) {
	a, err := netip.ParseAddr(ip)
	if err != nil || !agentproto.ValidPublicAddress(ip, a.Is6()) {
		return nil, errors.New("invalid public IP")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.country.is/"+a.String()+"?fields=city,subdivision,location", nil)
	if err != nil {
		return nil, err
	}
	// Apply the redirect boundary even to injected clients. A provider response
	// can never redirect a request containing the observed IP elsewhere.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("country service unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, errors.New("invalid country response")
	}
	var out struct {
		IP          string `json:"ip"`
		Country     string `json:"country"`
		City        string `json:"city"`
		Subdivision string `json:"subdivision"`
		Location    *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
		} `json:"location"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, errors.New("invalid country response")
	}
	returned, err := netip.ParseAddr(out.IP)
	if err != nil || returned != a {
		return nil, errors.New("country response IP mismatch")
	}
	if out.Country == "" {
		return nil, nil
	}
	location := &domain.GeoLocation{Country: out.Country, Subdivision: out.Subdivision, Precision: "country"}
	// Validate all returned labels and coordinates, even when there is not enough
	// information to draw a city. Country-centre coordinates are not city evidence.
	if out.City != "" || out.Location != nil {
		candidate := &domain.GeoLocation{Country: out.Country, Subdivision: out.Subdivision, City: out.City, Precision: "city"}
		if out.Location != nil {
			candidate.Lat, candidate.Lng = out.Location.Latitude, out.Location.Longitude
		}
		if candidate.City != "" && candidate.Lat != nil && candidate.Lng != nil {
			if candidate.Validate() != nil {
				return nil, errors.New("invalid country location")
			}
			location = candidate
		} else {
			// Missing fields are an ordinary country-only result. Invalid fields are not.
			check := candidate.Clone()
			if check.City == "" {
				check.City = "unknown"
			}
			zero := 0.0
			if check.Lat == nil {
				check.Lat = &zero
			}
			if check.Lng == nil {
				check.Lng = &zero
			}
			if check.Validate() != nil {
				return nil, errors.New("invalid country location")
			}
		}
	}
	if location.Validate() != nil {
		return nil, errors.New("invalid country location")
	}
	return location, nil
}

type NodeRegions struct {
	Store          *store.Store
	Probe          *Probe
	Lookup         func(context.Context, string) (string, error)              // legacy country-only injection
	LookupLocation func(context.Context, string) (*domain.GeoLocation, error) // nil uses Country.is unless Lookup is set
	once           sync.Once
	gate           chan struct{}
	nextRequest    time.Time // protected by gate
}

func (s *NodeRegions) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.Tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *NodeRegions) Tick(ctx context.Context, at time.Time) {
	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return
		}
		state, err := s.Store.NodeRegionState(ctx, n.ID)
		if err != nil || !state.Automatic() || !state.Fresh(at) {
			continue
		}
		_ = s.Detect(ctx, n.ID, false)
	}
}

// One bounded request stream across scheduled and interactive detection, below
// the provider's 10/s limit. Cached failures retry hourly, successes weekly.
func (s *NodeRegions) Detect(ctx context.Context, id int64, refresh bool) error {
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.gate }()
	state, err := s.Store.NodeRegionState(ctx, id)
	if err != nil {
		return err
	}
	at := time.Now()
	if !state.Fresh(at) || (!refresh && !state.Automatic()) {
		return nil
	}
	ip4, ip6 := state.IPs()
	d := state.Detection
	changed := false
	detect := func(ip string, result *store.RegionResult) error {
		if ip == "" {
			return nil
		}
		ttl := time.Hour
		if result.Status == "ok" {
			ttl = 7 * 24 * time.Hour
			// Old successful country-only caches get one bounded upgrade lookup.
			if result.LocationVersion == 0 {
				ttl = time.Minute
			}
		}
		if refresh {
			ttl = time.Minute
		}
		if result.IP == ip && result.CheckedAt > 0 && at.Sub(time.Unix(result.CheckedAt, 0)) < ttl {
			return nil
		}
		if delay := time.Until(s.nextRequest); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		s.nextRequest = time.Now().Add(150 * time.Millisecond)
		lookup := s.LookupLocation
		if lookup == nil {
			lookup = lookupLocation
			if s.Lookup != nil {
				lookup = func(ctx context.Context, ip string) (*domain.GeoLocation, error) {
					country, err := s.Lookup(ctx, ip)
					if err != nil || country == "" {
						return nil, err
					}
					return &domain.GeoLocation{Country: country, Precision: "country"}, nil
				}
			}
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		location, queryErr := lookup(queryCtx, ip)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if location != nil && location.Validate() != nil {
			queryErr = domain.ErrGeoLocation
		}
		previous := *result
		*result = store.RegionResult{IP: ip, CheckedAt: time.Now().Unix(), Status: "unknown", LocationVersion: 1}
		if queryErr != nil {
			result.Status = "failed"
			// A failed refresh may retain the last successful value for this IP only.
			// The attempt timestamp and status are separate from the successful sample.
			if previous.IP == ip && store.ValidRegion(previous.Country) && previous.LastSuccessAt > 0 {
				result.Country, result.Location, result.LastSuccessAt = previous.Country, previous.Location.Clone(), previous.LastSuccessAt
			}
		} else if location != nil {
			result.Country, result.Location, result.Status = location.Country, location.Clone(), "ok"
			result.LastSuccessAt = result.CheckedAt
		}
		changed = true
		return nil
	}
	if err = detect(ip4, &d.IPv4); err != nil {
		return err
	}
	if err = detect(ip6, &d.IPv6); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err = s.Store.SaveRegionDetection(ctx, state, d); err != nil {
		return err
	}
	if s.Probe != nil {
		s.Probe.Invalidate()
	}
	return nil
}
