package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrNodeRegion = errors.New("invalid region mode or country code")
var ErrRegionObservationChanged = errors.New("node address changed during region lookup")

// Use the same recognized two-letter country/territory codes for validation and UI.
var regionCountries = func() []string {
	out := []string{}
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			code := string([]rune{a, b})
			if domain.ValidCountryCode(code) {
				out = append(out, code)
			}
		}
	}
	return out
}()

func RegionCountries() []string    { return append([]string(nil), regionCountries...) }
func ValidRegion(code string) bool { return domain.ValidCountryCode(code) }

type RegionResult struct {
	Location        *domain.GeoLocation `json:"location,omitempty"`
	LocationVersion int                 `json:"location_version,omitempty"` // 1 queried city fields, including no-city results
	LastSuccessAt   int64               `json:"last_success_at,omitempty"`  // does not advance on a failed attempt
	IP              string              `json:"ip"`
	Country         string              `json:"country"`
	CheckedAt       int64               `json:"checked_at"`
	Status          string              `json:"status"` // ok | unknown | failed
}
type RegionDetection struct {
	Location      *domain.GeoLocation `json:"location,omitempty"`
	LastSuccessAt int64               `json:"last_success_at,omitempty"`
	Stale         bool                `json:"stale"`
	ObservedAt    int64               `json:"observed_at"`
	Fresh         bool                `json:"fresh"`
	IPv4          RegionResult        `json:"ipv4"`
	IPv6          RegionResult        `json:"ipv6"`
	Country       string              `json:"country"`
	IP            string              `json:"ip"`
	CheckedAt     int64               `json:"checked_at"`
	Conflict      bool                `json:"conflict"`
	Source        string              `json:"source"`
}

func observedRegionIPs(obs AddressObservation) (string, string) {
	valid := func(f AddressFamily, v6 bool) string {
		if f.CandidateSince > 0 && agentproto.ValidPublicAddress(f.Address, v6) {
			return f.Address
		}
		return ""
	}
	return valid(obs.IPv4, false), valid(obs.IPv6, true)
}
func regionMode(mode, manual string) string {
	if mode != "" {
		return mode
	}
	if manual != "" {
		return "manual"
	}
	return "off"
}

// Reconcile cached results with the current observation on every read. A changed
// or withdrawn IP must never display the country cached for its predecessor.
func resolveRegion(info *NodeProbeInfo, mode string, obs AddressObservation, d RegionDetection) (string, RegionDetection) {
	mode = regionMode(mode, info.Region)
	ip4, ip6 := observedRegionIPs(obs)
	match := func(r RegionResult, ip string) RegionResult {
		if r.IP != ip || ip == "" {
			return RegionResult{IP: ip}
		}
		if !ValidRegion(r.Country) || (r.Status != "ok" && r.LastSuccessAt == 0) {
			r.Country, r.Location, r.LastSuccessAt = "", nil, 0
		}
		if r.Status == "ok" && r.LastSuccessAt == 0 {
			r.LastSuccessAt = r.CheckedAt
		}
		if r.Location != nil && (r.Location.Validate() != nil || r.Location.Country != r.Country) {
			r.Location = nil
		}
		return r
	}
	d.IPv4, d.IPv6 = match(d.IPv4, ip4), match(d.IPv6, ip6)
	d.Source = "country.is"
	chosen := d.IPv4
	if ip4 == "" {
		chosen = d.IPv6
	}
	d.IP, d.Country, d.CheckedAt = chosen.IP, chosen.Country, chosen.CheckedAt
	d.LastSuccessAt = chosen.LastSuccessAt
	d.Stale = chosen.Country != "" && (chosen.Status != "ok" || chosen.LastSuccessAt < time.Now().Add(-7*24*time.Hour).Unix())
	d.Location = chosen.Location.Clone()
	if d.Location == nil && ValidRegion(chosen.Country) {
		d.Location = &domain.GeoLocation{Country: chosen.Country, Precision: "country"}
	}
	d.ObservedAt = obs.ReceivedAt
	d.Fresh = chosen.IP != "" && obs.ReceivedAt >= time.Now().Add(-15*time.Minute).Unix()
	d.Conflict = d.IPv4.Country != "" && d.IPv6.Country != "" && d.IPv4.Country != d.IPv6.Country
	switch mode {
	case "auto":
		info.Region = d.Country
	case "off":
		info.Region = ""
	}
	info.Location = nil
	switch nodeLocationMode(info.LocationSettings, mode) {
	case "auto":
		info.Location = d.Location.Clone()
	case "manual":
		if info.LocationSettings.Manual != nil && info.LocationSettings.Manual.Validate() == nil {
			info.Location = info.LocationSettings.Manual.Clone()
		}
	}
	return mode, d
}

func nodeLocationMode(settings *domain.LocationSettings, oldMode string) string {
	if settings != nil {
		return settings.Mode
	}
	if oldMode == "off" {
		return "off"
	}
	return "auto"
}

type NodeRegionState struct {
	LocationMode string
	ID           int64
	Token        string
	Mode         string
	Observed     AddressObservation
	Detection    RegionDetection
}

func (s *Store) NodeRegionState(ctx context.Context, id int64) (NodeRegionState, error) {
	out := NodeRegionState{ID: id}
	var obs, cache, info string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(token_hash,''),region_mode,address_json,region_detection_json,probe_info_json FROM nodes WHERE id=?`, id).Scan(&out.Token, &out.Mode, &obs, &cache, &info)
	if err != nil {
		return out, wrapNotFound(err)
	}
	if err = json.Unmarshal([]byte(obs), &out.Observed); err != nil {
		return out, err
	}
	_ = json.Unmarshal([]byte(cache), &out.Detection)
	var ni NodeProbeInfo
	_ = json.Unmarshal([]byte(info), &ni)
	out.Mode, out.Detection = resolveRegion(&ni, out.Mode, out.Observed, out.Detection)
	out.LocationMode = nodeLocationMode(ni.LocationSettings, out.Mode)
	return out, nil
}
func (n NodeRegionState) Automatic() bool       { return n.Mode == "auto" || n.LocationMode == "auto" }
func (n NodeRegionState) IPs() (string, string) { return observedRegionIPs(n.Observed) }
func (n NodeRegionState) Fresh(at time.Time) bool {
	return n.Token != "" && n.Observed.ReceivedAt >= at.Add(-15*time.Minute).Unix()
}
func (s *Store) SaveRegionDetection(ctx context.Context, expected NodeRegionState, d RegionDetection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var token, raw string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(token_hash,''),address_json FROM nodes WHERE id=?`, expected.ID).Scan(&token, &raw); err != nil {
		return wrapNotFound(err)
	}
	var obs AddressObservation
	if err = json.Unmarshal([]byte(raw), &obs); err != nil {
		return err
	}
	a, b := observedRegionIPs(obs)
	x, y := expected.IPs()
	if token == "" || token != expected.Token || a != x || b != y {
		return ErrRegionObservationChanged
	}
	buf, _ := json.Marshal(d)
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET region_detection_json=? WHERE id=?`, string(buf), expected.ID); err != nil {
		return err
	}
	return tx.Commit()
}
