package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestNodeLocationManualLegacyPreservationAndReadOnlyResult(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	n := &domain.Node{Name: "example"}
	if err := s.CreateNode(ctx, n, "location-test", time.Hour); err != nil {
		t.Fatal(err)
	}
	lat, lng := 0.0, 0.0
	manual := &domain.LocationSettings{Mode: "manual", Manual: &domain.GeoLocation{Country: " us ", City: " Example City ", Lat: &lat, Lng: &lng, Precision: "city"}}
	mode := "manual"
	write := func(info NodeProbeInfo) error {
		return s.UpdateNodeProbeWithRegion(ctx, n.ID, false, info, 0, 1, "sum", nil, &mode)
	}
	if err := write(NodeProbeInfo{Region: "DE", LocationSettings: manual}); err != nil {
		t.Fatal(err)
	}
	read := func() *NodeProbe {
		t.Helper()
		p, err := s.NodeProbe(ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := read()
	if p.Info.Region != "DE" || p.Info.Location == nil || p.Info.Location.Country != "US" || *p.Info.Location.Lat != 0 || p.Info.Location.City != "Example City" {
		t.Fatalf("independent country/location fields: %+v", p.Info)
	}
	// Old clients omit (or send null for) new private settings. The derived result
	// cannot be used to overwrite the saved configuration or spoof auto lookup.
	if err := write(NodeProbeInfo{Region: "DE", Location: &domain.GeoLocation{Country: "FR", Precision: "country"}}); err != nil {
		t.Fatal(err)
	}
	p = read()
	if p.Info.LocationSettings == nil || p.Info.LocationSettings.Mode != "manual" || p.Info.Location.Country != "US" {
		t.Fatal(p.Info)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT probe_info_json FROM nodes WHERE id=?`, n.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var persisted NodeProbeInfo
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Location != nil {
		t.Fatal("stored client-supplied location", raw)
	}
	if err := write(NodeProbeInfo{Region: "DE", LocationSettings: &domain.LocationSettings{Mode: "off", Manual: p.Info.LocationSettings.Manual}}); err != nil {
		t.Fatal(err)
	}
	p = read()
	if p.Info.Location != nil || p.Info.LocationSettings.Manual == nil || p.Info.Region != "DE" {
		t.Fatal(p.Info)
	}
	pub := p.Info.PublicInfo()
	if pub.LocationSettings != nil || pub.Location != nil {
		t.Fatal("off draft leaked", pub)
	}
	encoded, _ := json.Marshal(pub)
	var fields map[string]any
	_ = json.Unmarshal(encoded, &fields)
	if v, ok := fields["location"]; !ok || v != nil {
		t.Fatal("missing explicit disabled location", string(encoded))
	}
	if err := write(NodeProbeInfo{Region: "DE", LocationSettings: &domain.LocationSettings{Mode: "manual"}}); !errors.Is(err, domain.ErrGeoLocation) {
		t.Fatal(err)
	}
	if read().Info.LocationSettings.Mode != "off" {
		t.Fatal("failed write changed settings")
	}
}

func TestNodeLocationCountryFallbackAndAddressInvalidation(t *testing.T) {
	at := time.Now().Unix()
	obs := AddressObservation{ReceivedAt: at, IPv4: AddressFamily{Address: "198.51.100.20", CandidateSince: at}}
	detection := RegionDetection{IPv4: RegionResult{IP: "198.51.100.20", Country: "JP", Status: "ok", CheckedAt: at}}
	info := NodeProbeInfo{}
	_, d := resolveRegion(&info, "auto", obs, detection)
	if info.Location == nil || info.Location.Precision != "country" || info.Location.Country != "JP" || info.Location.Lat != nil {
		t.Fatal(info)
	}
	if d.LastSuccessAt != at {
		t.Fatal("legacy success timestamp lost", d)
	}
	obs.IPv4.Address = "203.0.113.30"
	resolveRegion(&info, "auto", obs, detection)
	if info.Location != nil || info.Region != "" {
		t.Fatal("old IP location survived", info)
	}
	obs.IPv4.Address = "198.51.100.20"
	resolveRegion(&info, "off", obs, detection)
	if info.Location != nil {
		t.Fatal("legacy off enabled location", info)
	}
	info.LocationSettings = &domain.LocationSettings{Mode: "auto"}
	resolveRegion(&info, "off", obs, detection)
	if info.Region != "" || info.Location == nil {
		t.Fatal("explicit location auto depended on flag mode", info)
	}
}

func TestNodeLocationRetainsLegacyManualCountryWithoutObservation(t *testing.T) {
	info := NodeProbeInfo{Region: "US"}
	resolveRegion(&info, "manual", AddressObservation{}, RegionDetection{})
	if info.Location == nil || info.Location.Country != "US" || info.Location.Precision != "country" || info.Location.Lat != nil {
		t.Fatal("lost legacy country marker", info)
	}
	info.LocationSettings = &domain.LocationSettings{Mode: "auto"}
	resolveRegion(&info, "manual", AddressObservation{}, RegionDetection{})
	if info.Location != nil {
		t.Fatal("explicit automatic location borrowed flag", info)
	}
}
