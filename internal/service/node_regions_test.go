package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeRegionsCacheModesAndAddressChanges(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Add(-time.Minute)
	observe := func(ip4, ip6 string) {
		t.Helper()
		at = at.Add(time.Second)
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: ip4, IPv6: ip6}, at))
	}
	read := func() *store.NodeProbe { p, err := st.NodeProbe(ctx, n.ID); must(err); return p }
	set := func(mode, country string) {
		must(st.UpdateNodeProbeWithRegion(ctx, n.ID, false, store.NodeProbeInfo{Region: country, LocationSettings: &domain.LocationSettings{Mode: "off"}}, 0, 1, "sum", nil, &mode))
	}
	calls := 0
	svc := &NodeRegions{Store: st, Lookup: func(ctx context.Context, ip string) (string, error) {
		calls++
		if !st.Accounts.TryLock() {
			t.Error("network holds Accounts")
		} else {
			st.Accounts.Unlock()
		}
		if !st.Topology.TryLock() {
			t.Error("network holds Topology")
		} else {
			st.Topology.Unlock()
		}
		if ip == "2001:db8::1" {
			return "US", nil
		}
		return "JP", nil
	}}
	if p := read(); p.RegionMode != "auto" || p.Info.Region != "" {
		t.Fatal(p)
	}
	// Fixed entry addresses are never looked up when no observation exists.
	must(svc.Detect(ctx, n.ID, true))
	if calls != 0 {
		t.Fatal(calls)
	}
	observe("198.51.100.20", "2001:db8::1")
	must(svc.Detect(ctx, n.ID, false))
	p := read()
	if p.Info.Region != "JP" || !p.RegionDetection.Conflict || p.RegionDetection.IP != "198.51.100.20" {
		t.Fatal(p)
	}
	must(svc.Detect(ctx, n.ID, false))
	must(svc.Detect(ctx, n.ID, true))
	if calls != 2 {
		t.Fatal("cache bypass", calls)
	}
	// A legacy client echoing the public result must not switch automatic mode off.
	must(st.UpdateNodeProbe(ctx, n.ID, false, p.Info, 100, 1, "sum"))
	if read().RegionMode != "auto" {
		t.Fatal("legacy echo lost auto")
	}
	set("manual", "DE")
	observe("203.0.113.30", "")
	if p = read(); p.Info.Region != "DE" || p.RegionDetection.Country != "" {
		t.Fatal("manual/IP change", p)
	}
	svc.Tick(ctx, time.Now())
	must(svc.Detect(ctx, n.ID, false))
	if calls != 2 {
		t.Fatal("manual scheduled lookup")
	}
	set("off", "")
	must(svc.Detect(ctx, n.ID, true))
	if p = read(); p.Info.Region != "" || p.RegionDetection.Country != "JP" {
		t.Fatal("off overwritten", p)
	}
	set("auto", "")
	if read().Info.Region != "JP" {
		t.Fatal("cached result not used")
	}
	observe("192.0.2.10", "")
	if read().Info.Region != "" {
		t.Fatal("old IP country leaked")
	}
	svc.Lookup = func(context.Context, string) (string, error) { calls++; return "", errors.New("failed") }
	must(svc.Detect(ctx, n.ID, false))
	p = read()
	if p.Info.Region != "" || p.RegionDetection.IPv4.Status != "failed" {
		t.Fatal(p)
	}
	count := calls
	must(svc.Detect(ctx, n.ID, false))
	if calls != count {
		t.Fatal("failed lookup not cached")
	}
	// Missing IPv4 selects IPv6; a failed IPv4 lookup does not silently switch families.
	svc.Lookup = func(context.Context, string) (string, error) { return "US", nil }
	observe("192.0.2.10", "2001:db8::1")
	must(svc.Detect(ctx, n.ID, false))
	if p = read(); p.Info.Region != "" || p.RegionDetection.IPv6.Country != "US" {
		t.Fatal("failed IPv4 silently fell back", p)
	}
	observe("", "2001:db8::1")
	must(svc.Detect(ctx, n.ID, false))
	if p = read(); p.Info.Region != "US" || p.RegionDetection.IP != "2001:db8::1" {
		t.Fatal(p)
	}
	// A legacy user changing country still expresses a manual override.
	must(st.UpdateNodeProbe(ctx, n.ID, false, store.NodeProbeInfo{Region: "FR"}, 0, 1, "sum"))
	if p = read(); p.RegionMode != "manual" || p.Info.Region != "FR" {
		t.Fatal(p)
	}
}

func TestNodeRegionsRejectLateResultsWithoutOverwritingMode(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Add(-time.Minute)
	observe := func(ip string) {
		at = at.Add(time.Second)
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: ip}, at))
	}
	observe("198.51.100.20")
	svc := &NodeRegions{Store: st, Lookup: func(context.Context, string) (string, error) { observe("203.0.113.30"); return "JP", nil }}
	if err = svc.Detect(ctx, n.ID, false); !errors.Is(err, store.ErrRegionObservationChanged) {
		t.Fatal(err)
	}
	p, _ := st.NodeProbe(ctx, n.ID)
	if p.Info.Region != "" {
		t.Fatal(p)
	}
	svc.Lookup = func(context.Context, string) (string, error) {
		mode := "manual"
		must(st.UpdateNodeProbeWithRegion(ctx, n.ID, false, store.NodeProbeInfo{Region: "DE"}, 0, 1, "sum", nil, &mode))
		return "JP", nil
	}
	must(svc.Detect(ctx, n.ID, false))
	p, _ = st.NodeProbe(ctx, n.ID)
	if p.Info.Region != "DE" || p.RegionMode != "manual" {
		t.Fatal("late result overwrote manual", p)
	}
	// Re-pair/identity changes cannot accept a previously started lookup.
	old, err := st.NodeRegionState(ctx, n.ID)
	must(err)
	wrongIdentity := old
	wrongIdentity.Token = "other-pairing"
	if err = st.SaveRegionDetection(ctx, wrongIdentity, old.Detection); !errors.Is(err, store.ErrRegionObservationChanged) {
		t.Fatal(err)
	}
	// A cleared observation models the same invalidation as repair without touching DNS.
	observe("")
	if err = st.SaveRegionDetection(ctx, old, old.Detection); !errors.Is(err, store.ErrRegionObservationChanged) {
		t.Fatal(err)
	}
}

func TestNodeRegionsOfflineKeepsCountryWithoutQuerying(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute)
	if err = st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: "198.51.100.20"}, at); err != nil {
		t.Fatal(err)
	}
	state, err := st.NodeRegionState(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SaveRegionDetection(ctx, state, store.RegionDetection{IPv4: store.RegionResult{IP: "198.51.100.20", Country: "JP", Status: "ok", CheckedAt: at.Unix()}}); err != nil {
		t.Fatal(err)
	}
	// Time passes after the successful lookup: stale observations are readable
	// cache history, but must no longer be accepted as new lookup evidence.
	observed := state.Observed
	observed.IPv4.LastSuccess = time.Now().Add(-16 * time.Minute).Unix()
	observed.SampledAt = observed.IPv4.LastSuccess
	raw, _ := json.Marshal(observed)
	if _, err = st.DB().Exec(`UPDATE nodes SET address_json=? WHERE id=?`, string(raw), n.ID); err != nil {
		t.Fatal(err)
	}
	svc := &NodeRegions{Store: st, Lookup: func(context.Context, string) (string, error) { t.Fatal("stale address queried"); return "", nil }}
	if err = svc.Detect(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	p, err := st.NodeProbe(ctx, n.ID)
	if err != nil || p.Info.Region != "JP" || p.RegionDetection.Fresh {
		t.Fatal(p, err)
	}
}

func TestNodeLocationsUpgradeCacheAndRetainLastGood(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Add(-2 * time.Minute)
	observe := func(ip string) {
		t.Helper()
		at = at.Add(time.Second)
		must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: ip}, at))
	}
	observe("198.51.100.20")
	state, err := st.NodeRegionState(ctx, n.ID)
	must(err)
	must(st.SaveRegionDetection(ctx, state, store.RegionDetection{IPv4: store.RegionResult{IP: "198.51.100.20", Country: "JP", Status: "ok", CheckedAt: time.Now().Add(-2 * time.Hour).Unix()}}))
	calls := 0
	lat, lng := 35.68, 139.76
	svc := &NodeRegions{Store: st, LookupLocation: func(context.Context, string) (*domain.GeoLocation, error) {
		calls++
		if !st.Accounts.TryLock() {
			t.Error("network held Accounts")
		} else {
			st.Accounts.Unlock()
		}
		if !st.Topology.TryLock() {
			t.Error("network held Topology")
		} else {
			st.Topology.Unlock()
		}
		return &domain.GeoLocation{Country: "JP", City: "Tokyo", Lat: &lat, Lng: &lng, Precision: "city"}, nil
	}}
	must(svc.Detect(ctx, n.ID, false))
	p, err := st.NodeProbe(ctx, n.ID)
	must(err)
	if calls != 1 || p.Info.Location == nil || p.Info.Location.City != "Tokyo" || p.RegionDetection.IPv4.LocationVersion != 1 {
		t.Fatal(calls, p)
	}
	must(svc.Detect(ctx, n.ID, false))
	if calls != 1 {
		t.Fatal("city cache not retained", calls)
	}
	// A failed weekly refresh retains the coordinates for this IP, but does not
	// present the failed attempt as a fresh successful location sample.
	state, err = st.NodeRegionState(ctx, n.ID)
	must(err)
	oldAt := time.Now().Add(-8 * 24 * time.Hour).Unix()
	state.Detection.IPv4.CheckedAt = oldAt
	state.Detection.IPv4.LastSuccessAt = oldAt
	must(st.SaveRegionDetection(ctx, state, state.Detection))
	svc.LookupLocation = func(context.Context, string) (*domain.GeoLocation, error) {
		calls++
		return nil, errors.New("provider unavailable")
	}
	must(svc.Detect(ctx, n.ID, false))
	p, err = st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Location == nil || p.Info.Location.City != "Tokyo" || p.RegionDetection.IPv4.Status != "failed" || p.RegionDetection.LastSuccessAt != oldAt || !p.RegionDetection.Stale || p.RegionDetection.CheckedAt <= oldAt {
		t.Fatal(p)
	}
	count := calls
	must(svc.Detect(ctx, n.ID, false))
	if calls != count {
		t.Fatal("failed lookup bypassed hourly backoff")
	}
	observe("203.0.113.30")
	p, err = st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Location != nil || p.RegionDetection.Location != nil {
		t.Fatal("old location on changed IP", p)
	}
	must(svc.Detect(ctx, n.ID, false))
	p, err = st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Location != nil || p.RegionDetection.LastSuccessAt != 0 {
		t.Fatal("failed new IP inherited last-good", p)
	}
}

func TestNodeLocationsIndependentModesAndLateManualChoice(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Add(-time.Minute)
	must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: at.Unix(), IPv4: "198.51.100.20"}, at))
	mode := "manual"
	must(st.UpdateNodeProbeWithRegion(ctx, n.ID, false, store.NodeProbeInfo{Region: "DE"}, 0, 1, "sum", nil, &mode))
	lat, lng := 35.68, 139.76
	svc := &NodeRegions{Store: st, LookupLocation: func(context.Context, string) (*domain.GeoLocation, error) {
		// A manual location chosen while the provider is in flight stays authoritative.
		manual := &domain.LocationSettings{Mode: "manual", Manual: &domain.GeoLocation{Country: "US", City: "Example City", Lat: &lat, Lng: &lng, Precision: "city"}}
		must(st.UpdateNodeProbeWithRegion(ctx, n.ID, false, store.NodeProbeInfo{Region: "DE", LocationSettings: manual}, 0, 1, "sum", nil, &mode))
		return &domain.GeoLocation{Country: "JP", City: "Tokyo", Lat: &lat, Lng: &lng, Precision: "city"}, nil
	}}
	// Legacy manual flags now have automatic city detection until a location mode
	// is explicitly chosen, without changing the independent manual flag.
	svc.Tick(ctx, time.Now())
	p, err := st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Region != "DE" || p.Info.Location == nil || p.Info.Location.Country != "US" || p.Info.Location.City != "Example City" || p.RegionDetection.Location.City != "Tokyo" {
		t.Fatal(p)
	}
	svc.LookupLocation = func(context.Context, string) (*domain.GeoLocation, error) {
		t.Fatal("manual location and flag triggered query")
		return nil, nil
	}
	svc.Tick(ctx, time.Now())
}

func TestNodeLocationIPManualEntryAndLatePolicyResult(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	set := func(settings store.LocationIPSettings) {
		t.Helper()
		mode := "auto"
		must(st.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, store.NodeProbeInfo{}, 0, 1, "sum", nil, &mode, nil, store.NodeInfrastructureGuard{}, &settings))
	}
	set(store.LocationIPSettings{Mode: "manual", ManualIP: "203.0.113.30"})
	calls := []string{}
	svc := &NodeRegions{Store: st, Lookup: func(_ context.Context, ip string) (string, error) { calls = append(calls, ip); return "JP", nil }}
	// This server has not paired and has no public observations.
	must(svc.Detect(ctx, n.ID, false))
	if len(calls) != 1 || calls[0] != "203.0.113.30" {
		t.Fatal(calls)
	}
	p, err := st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Region != "JP" || p.RegionDetection.IPSource != "manual" {
		t.Fatal(p)
	}
	set(store.LocationIPSettings{Mode: "entry"})
	state, err := st.NodeRegionState(ctx, n.ID)
	must(err)
	a, b := state.IPs()
	if a == "" && b == "" {
		t.Fatal("fixture needs an existing entry")
	}
	must(svc.Detect(ctx, n.ID, true))
	p, err = st.NodeProbe(ctx, n.ID)
	must(err)
	if p.RegionDetection.IPSource != "entry_override" || p.Info.Region != "JP" {
		t.Fatal(p)
	}
	set(store.LocationIPSettings{Mode: "manual", ManualIP: "192.0.2.99"})
	svc.Lookup = func(context.Context, string) (string, error) {
		set(store.LocationIPSettings{Mode: "auto"})
		set(store.LocationIPSettings{Mode: "manual", ManualIP: "192.0.2.99"})
		return "US", nil
	}
	if err = svc.Detect(ctx, n.ID, true); !errors.Is(err, store.ErrRegionObservationChanged) {
		t.Fatal("late policy ABA lookup accepted", err)
	}
	p, err = st.NodeProbe(ctx, n.ID)
	must(err)
	if p.Info.Region != "" {
		t.Fatal("late location published", p)
	}
}

func TestNodeLocationIPRejectsLookupCrossingPrimaryExpiryIntoEntryFallback(t *testing.T) {
	ctx := context.Background()
	st, n := dnsTestStore(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := st.RedeemPairCode(ctx, "dns-test", "token", "node", "test", "linux")
	must(err)
	mode := "auto"
	must(st.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, store.NodeProbeInfo{}, 0, 1, "sum", nil, &mode, nil, store.NodeInfrastructureGuard{}, &store.LocationIPSettings{Mode: "auto", AllowEntryFallback: true}))
	// Construct a valid primary sample that expires while the provider request
	// is in flight. The explicit entry remains available throughout; no write or
	// revision change is needed for the effective source to switch with time.
	lastSuccess := time.Now().Truncate(time.Second).Add(-15*time.Minute + time.Second)
	must(st.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: lastSuccess.Unix(), IPv4: "198.51.100.20"}, lastSuccess))
	started := make(chan struct{})
	svc := &NodeRegions{Store: st, Lookup: func(ctx context.Context, ip string) (string, error) {
		if ip != "198.51.100.20" {
			t.Errorf("lookup changed address before starting: %s", ip)
		}
		close(started)
		timer := time.NewTimer(time.Until(lastSuccess.Add(15*time.Minute + time.Second)))
		defer timer.Stop()
		select {
		case <-timer.C:
			return "JP", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	if err = svc.Detect(ctx, n.ID, true); !errors.Is(err, store.ErrRegionObservationChanged) {
		t.Fatal("accepted stale primary result under entry fallback", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("lookup never started")
	}
	state, err := st.NodeRegionState(ctx, n.ID)
	must(err)
	if state.Detection.IPSource != "entry_fallback" || !state.Fresh(time.Now()) {
		t.Fatal("fallback should now be valid", state)
	}
	var cache string
	must(st.DB().QueryRow(`SELECT region_detection_json FROM nodes WHERE id=?`, n.ID).Scan(&cache))
	var detection store.RegionDetection
	must(json.Unmarshal([]byte(cache), &detection))
	if detection.IPv4.Country != "" {
		t.Fatal("obsolete primary lookup persisted", cache)
	}
}
