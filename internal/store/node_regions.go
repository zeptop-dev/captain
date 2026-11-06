package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrLocationIP = errors.New("invalid location IP settings: use auto, manual with a public IP, or entry")
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
	IPSource        string              `json:"ip_source"`
	SelectionReason string              `json:"selection_reason"`
	Location        *domain.GeoLocation `json:"location,omitempty"`
	LastSuccessAt   int64               `json:"last_success_at,omitempty"`
	Stale           bool                `json:"stale"`
	ObservedAt      int64               `json:"observed_at"`
	Fresh           bool                `json:"fresh"`
	IPv4            RegionResult        `json:"ipv4"`
	IPv6            RegionResult        `json:"ipv6"`
	Country         string              `json:"country"`
	IP              string              `json:"ip"`
	CheckedAt       int64               `json:"checked_at"`
	Conflict        bool                `json:"conflict"`
	Source          string              `json:"source"`
}

// LocationIPSettings belongs to the administrator's private server profile,
// not NodeProbeInfo, which is also used by public read models.
type LocationIPSettings struct {
	Mode               string `json:"mode"`
	ManualIP           string `json:"manual_ip"`
	AllowEntryFallback bool   `json:"allow_entry_fallback"`
}

func (s *LocationIPSettings) normalize() {
	if s.Mode == "" {
		s.Mode = "auto"
	}
	s.ManualIP = strings.TrimSpace(s.ManualIP)
	if a, err := netip.ParseAddr(s.ManualIP); err == nil {
		s.ManualIP = a.String()
	}
}
func (s LocationIPSettings) Validate() error {
	if s.Mode != "auto" && s.Mode != "manual" && s.Mode != "entry" {
		return ErrLocationIP
	}
	if s.ManualIP != "" && validLocationIP(s.ManualIP) == "" {
		return ErrLocationIP
	}
	if s.Mode == "manual" && s.ManualIP == "" {
		return ErrLocationIP
	}
	return nil
}
func validLocationIP(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !agentproto.ValidPublicAddress(ip, a.Is6()) {
		return ""
	}
	return a.String()
}

type locationIPInput struct {
	Settings  LocationIPSettings
	Revision  int64
	Token     string
	Addresses NodeAddresses
	raw       string
}

const locationIPColumns = `location_ip_json,location_ip_revision,COALESCE(token_hash,''),public_addr,v6_addr,address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6`

func (i *locationIPInput) scanArgs() []any {
	return []any{&i.raw, &i.Revision, &i.Token, &i.Addresses.EntryIPv4, &i.Addresses.EntryIPv6, &i.Addresses.AutoIPv4, &i.Addresses.AutoIPv6, &i.Addresses.FallbackAutoIPv4, &i.Addresses.FallbackAutoIPv6}
}
func (i *locationIPInput) decode(obs AddressObservation) {
	_ = json.Unmarshal([]byte(i.raw), &i.Settings)
	i.Settings.normalize()
	i.Addresses.Observed = obs
}

type locationIPSelection struct {
	IPv4, IPv6, Source, Reason string
	ObservedAt                 int64
	Fresh                      bool
}

func (s locationIPSelection) ips() (string, string) { return s.IPv4, s.IPv6 }
func (i locationIPInput) selectIP(at time.Time) locationIPSelection {
	result := locationIPSelection{Reason: "no_available_ip"}
	pair := func(a, b, source string) locationIPSelection {
		a, b = validLocationIP(a), validLocationIP(b)
		if a != "" && !agentproto.ValidPublicAddress(a, false) {
			a = ""
		}
		if b != "" && !agentproto.ValidPublicAddress(b, true) {
			b = ""
		}
		return locationIPSelection{IPv4: a, IPv6: b, Source: source, Reason: "selected", Fresh: a != "" || b != ""}
	}
	entry := func(source string) locationIPSelection {
		return pair(i.Addresses.EntryIPv4, i.Addresses.EntryIPv6, source)
	}
	switch i.Settings.Mode {
	case "manual":
		ip := validLocationIP(i.Settings.ManualIP)
		if ip == "" {
			result.Reason = "manual_ip_invalid"
			return result
		}
		if agentproto.ValidPublicAddress(ip, false) {
			return pair(ip, "", "manual")
		}
		return pair("", ip, "manual")
	case "entry":
		result = entry("entry_override")
		if !result.Fresh {
			result.Reason = "entry_unavailable"
		}
		return result
	case "auto", "":
	default:
		return result
	}
	obs := i.Addresses.Observed
	// Per-family success timestamps are essential: an unrelated heartbeat or a
	// successful IPv6 report must not renew an old IPv4 observation.
	primary := func(f AddressFamily, v6 bool, fresh bool) string {
		if i.Token == "" || f.CandidateSince <= 0 || !agentproto.ValidPublicAddress(f.Address, v6) {
			return ""
		}
		if fresh && (f.LastSuccess < at.Add(-15*time.Minute).Unix() || f.LastSuccess > at.Unix() || obs.SampledAt < at.Add(-15*time.Minute).Unix() || obs.SampledAt > at.Add(time.Minute).Unix()) {
			return ""
		}
		return f.Address
	}
	result = pair(primary(obs.IPv4, false, true), primary(obs.IPv6, true, true), "primary")
	if result.Fresh {
		result.ObservedAt = obs.IPv4.LastSuccess
		if result.IPv4 == "" {
			result.ObservedAt = obs.IPv6.LastSuccess
		}
		return result
	}
	fallback := func(f FallbackFollow, sample *AddressFallback, enabled, v6 bool) string {
		if i.Token != "" && enabled && agentproto.ValidPublicAddress(f.Address, v6) && validFallbackFollow(f, sample, at) {
			return f.Address
		}
		return ""
	}
	result = pair(fallback(obs.FallbackFollowIPv4, obs.FallbackIPv4, i.Addresses.AutoIPv4 && i.Addresses.FallbackAutoIPv4, false), fallback(obs.FallbackFollowIPv6, obs.FallbackIPv6, i.Addresses.AutoIPv6 && i.Addresses.FallbackAutoIPv6, true), "fallback")
	if result.Fresh {
		result.ObservedAt = obs.FallbackFollowIPv4.LastSuccess
		if result.IPv4 == "" {
			result.ObservedAt = obs.FallbackFollowIPv6.LastSuccess
		}
		return result
	}
	if i.Settings.AllowEntryFallback {
		result = entry("entry_fallback")
		if result.Fresh {
			return result
		}
	}
	// Preserve an offline primary observation only as a historical cache key.
	// It is never eligible for a new lookup, and withdrawn observations clear it.
	result = pair(primary(obs.IPv4, false, false), primary(obs.IPv6, true, false), "primary")
	result.Fresh = false
	result.Reason = "no_available_ip"
	if result.IPv4 != "" || result.IPv6 != "" {
		result.Reason = "observations_stale"
		result.ObservedAt = obs.IPv4.LastSuccess
		if result.IPv4 == "" {
			result.ObservedAt = obs.IPv6.LastSuccess
		}
	} else {
		result.Source = ""
	}
	return result
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
func resolveRegion(info *NodeProbeInfo, mode string, obs AddressObservation, d RegionDetection, inputs ...locationIPInput) (string, RegionDetection) {
	mode = regionMode(mode, info.Region)
	input := locationIPInput{Token: "legacy", Addresses: NodeAddresses{Observed: obs}}
	if len(inputs) > 0 {
		input = inputs[0]
	}
	selection := input.selectIP(time.Now())
	ip4, ip6 := selection.ips()
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
	d.Stale = chosen.Country != "" && (!selection.Fresh || chosen.Status != "ok" || chosen.LastSuccessAt < time.Now().Add(-7*24*time.Hour).Unix())
	d.Location = chosen.Location.Clone()
	if d.Location == nil && ValidRegion(chosen.Country) {
		d.Location = &domain.GeoLocation{Country: chosen.Country, Precision: "country"}
	}
	d.IPSource, d.SelectionReason = selection.Source, selection.Reason
	d.ObservedAt = selection.ObservedAt
	d.Fresh = selection.Fresh
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
		// Older manually assigned country markers remain useful on agents that
		// have never reported a public address. Keep only that explicit country
		// approximation until automatic city data is available. An explicitly
		// selected auto mode never borrows the independent flag field.
		if info.Location == nil && info.LocationSettings == nil && mode == "manual" && ValidRegion(info.Region) {
			info.Location = &domain.GeoLocation{Country: info.Region, Precision: "country"}
		}
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
	incarnation  string
	input        locationIPInput
	selection    locationIPSelection
	LocationMode string
	ID           int64
	Token        string
	Mode         string
	Observed     AddressObservation
	Detection    RegionDetection
}

const nodeRegionStateColumns = `region_mode,address_json,region_detection_json,probe_info_json,` + locationIPColumns + `,COALESCE((SELECT uuid FROM probe_public_ids WHERE node_id=nodes.id),'')`

func scanNodeRegionState(row interface{ Scan(...any) error }, id int64) (NodeRegionState, error) {
	out := NodeRegionState{ID: id}
	var obs, cache, info string
	args := append([]any{&out.Mode, &obs, &cache, &info}, out.input.scanArgs()...)
	args = append(args, &out.incarnation)
	if err := row.Scan(args...); err != nil {
		return out, wrapNotFound(err)
	}
	if err := json.Unmarshal([]byte(obs), &out.Observed); err != nil {
		return out, err
	}
	out.input.decode(out.Observed)
	out.Token = out.input.Token
	out.selection = out.input.selectIP(time.Now())
	_ = json.Unmarshal([]byte(cache), &out.Detection)
	var ni NodeProbeInfo
	_ = json.Unmarshal([]byte(info), &ni)
	out.Mode, out.Detection = resolveRegion(&ni, out.Mode, out.Observed, out.Detection, out.input)
	out.LocationMode = nodeLocationMode(ni.LocationSettings, out.Mode)
	return out, nil
}
func (s *Store) NodeRegionState(ctx context.Context, id int64) (NodeRegionState, error) {
	return scanNodeRegionState(s.db.QueryRowContext(ctx, `SELECT `+nodeRegionStateColumns+` FROM nodes WHERE id=?`, id), id)
}
func (n NodeRegionState) Automatic() bool       { return n.Mode == "auto" || n.LocationMode == "auto" }
func (n NodeRegionState) IPs() (string, string) { return n.selection.ips() }
func (n NodeRegionState) Fresh(at time.Time) bool {
	current := n.input.selectIP(at)
	return n.incarnation != "" && current.Fresh && current.Source == n.selection.Source && current.IPv4 == n.selection.IPv4 && current.IPv6 == n.selection.IPv6
}
func (s *Store) SaveRegionDetection(ctx context.Context, expected NodeRegionState, d RegionDetection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanNodeRegionState(tx.QueryRowContext(ctx, `SELECT `+nodeRegionStateColumns+` FROM nodes WHERE id=?`, expected.ID), expected.ID)
	if err != nil {
		return err
	}
	at := time.Now()
	before, after := expected.selection, current.input.selectIP(at)
	// A monotonic persisted revision catches policy/address A→B→A; current
	// freshness catches evidence expiring during the provider request. Pairing
	// identity remains relevant even for explicit location overrides. The UUID
	// also binds unpaired overrides to one incarnation: SQLite row IDs and
	// per-row revisions can both be reused after deletion.
	if current.incarnation == "" || current.incarnation != expected.incarnation || current.Token != expected.Token || current.input.Revision != expected.input.Revision || !expected.Fresh(at) || !after.Fresh || before.Source != after.Source || before.IPv4 != after.IPv4 || before.IPv6 != after.IPv6 {
		return ErrRegionObservationChanged
	}
	buf, _ := json.Marshal(d)
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET region_detection_json=? WHERE id=?`, string(buf), expected.ID); err != nil {
		return err
	}
	return tx.Commit()
}
