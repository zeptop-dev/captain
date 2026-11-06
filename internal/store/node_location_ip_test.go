package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

func locationIPFixture(at time.Time) locationIPInput {
	sample := fallbackFollowSample(at, "203.0.113.30", false).FallbackIPv4
	return locationIPInput{Token: "paired", Settings: LocationIPSettings{Mode: "auto"}, Addresses: NodeAddresses{
		AutoIPv4: true, FallbackAutoIPv4: true, EntryIPv4: "192.0.2.10", EntryIPv6: "2001:db8::10",
		Observed: AddressObservation{SampledAt: at.Unix(), ReceivedAt: at.Unix(), IPv4: AddressFamily{Address: "198.51.100.20", CandidateSince: at.Add(-time.Minute).Unix(), LastSuccess: at.Unix()}, IPv6: AddressFamily{Address: "2001:db8::20", CandidateSince: at.Add(-time.Minute).Unix(), LastSuccess: at.Unix()},
			FallbackIPv4:       &AddressFallback{PublicAddressFallback: *sample, ReceivedAt: at.Unix()},
			FallbackFollowIPv4: FallbackFollow{Address: sample.Address, Source: sample.Source, Origin: sample.Origin, RunID: sample.RunID, CandidateSince: at.Add(-3 * time.Minute).Unix(), CandidateSampledAt: at.Add(-3 * time.Minute).Unix(), LastSampledAt: at.Unix(), LastSuccess: at.Unix(), ConfirmedAt: at.Unix()},
		},
	}}
}
func TestLocationIPSelectionPriorityAndEvidence(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	tests := []struct {
		name       string
		edit       func(*locationIPInput)
		source, ip string
		fresh      bool
	}{
		{"ordinary dual stack", nil, "primary", "198.51.100.20", true},
		{"manual takes precedence", func(i *locationIPInput) { i.Settings = LocationIPSettings{Mode: "manual", ManualIP: "2001:db8::30"} }, "manual", "2001:db8::30", true},
		{"manual without agent", func(i *locationIPInput) {
			i.Token = ""
			i.Settings = LocationIPSettings{Mode: "manual", ManualIP: "203.0.113.30"}
		}, "manual", "203.0.113.30", true},
		{"explicit entry without agent", func(i *locationIPInput) { i.Token = ""; i.Settings.Mode = "entry" }, "entry_override", "192.0.2.10", true},
		{"primary independent of following", func(i *locationIPInput) { i.Addresses.AutoIPv4 = false }, "primary", "198.51.100.20", true},
		{"fresh primary IPv6 beats fallback IPv4", func(i *locationIPInput) { i.Addresses.Observed.IPv4.CandidateSince = 0 }, "primary", "2001:db8::20", true},
		{"stale IPv4 cannot borrow IPv6 report", func(i *locationIPInput) { i.Addresses.Observed.IPv4.LastSuccess = at.Add(-time.Hour).Unix() }, "primary", "2001:db8::20", true},
		{"approved fallback", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
		}, "fallback", "203.0.113.30", true},
		{"fallback opt out", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
			i.Addresses.FallbackAutoIPv4 = false
		}, "", "", false},
		{"follow opt out", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
			i.Addresses.AutoIPv4 = false
		}, "", "", false},
		{"entry fallback opt in", func(i *locationIPInput) {
			i.Addresses.Observed = AddressObservation{}
			i.Settings.AllowEntryFallback = true
		}, "entry_fallback", "192.0.2.10", true},
		{"entry fallback defaults closed", func(i *locationIPInput) { i.Addresses.Observed = AddressObservation{} }, "", "", false},
		{"fallback conflict", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
			i.Addresses.Observed.FallbackIPv4.CheckStatus = "conflict"
		}, "", "", false},
		{"fallback no run identity", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
			i.Addresses.Observed.FallbackFollowIPv4.RunID = ""
		}, "", "", false},
		{"fallback expired", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4 = AddressFamily{}
			i.Addresses.Observed.IPv6 = AddressFamily{}
			i.Addresses.Observed.FallbackFollowIPv4.LastSuccess = at.Add(-time.Hour).Unix()
		}, "", "", false},
		{"unpaired primary excluded", func(i *locationIPInput) { i.Token = "" }, "", "", false},
		{"clock rollback not fresh", func(i *locationIPInput) {
			i.Addresses.Observed.IPv4.LastSuccess = at.Add(time.Hour).Unix()
			i.Addresses.Observed.IPv6.LastSuccess = at.Add(time.Hour).Unix()
			i.Addresses.FallbackAutoIPv4 = false
		}, "primary", "198.51.100.20", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := locationIPFixture(at)
			if tt.edit != nil {
				tt.edit(&in)
			}
			got := in.selectIP(at)
			ip := got.IPv4
			if ip == "" {
				ip = got.IPv6
			}
			if got.Source != tt.source || ip != tt.ip || got.Fresh != tt.fresh {
				t.Fatalf("%+v", got)
			}
		})
	}
}
func TestLocationIPSavePoliciesABAAndProjection(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	n := &domain.Node{Name: "example", PublicAddr: "192.0.2.10"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateNode(ctx, n, "location-ip", time.Hour))
	set := func(settings *LocationIPSettings) {
		t.Helper()
		mode := "auto"
		must(s.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, NodeProbeInfo{}, 0, 1, "sum", nil, &mode, nil, NodeInfrastructureGuard{}, settings))
	}
	set(&LocationIPSettings{Mode: "manual", ManualIP: "198.51.100.20"})
	state, err := s.NodeRegionState(ctx, n.ID)
	must(err)
	if !state.Fresh(time.Now()) {
		t.Fatal("manual override depends on agent")
	}
	d := RegionDetection{IPv4: RegionResult{IP: "198.51.100.20", Country: "JP", Status: "ok", CheckedAt: time.Now().Unix()}}
	must(s.SaveRegionDetection(ctx, state, d))
	// No setting in a legacy request preserves the private policy.
	set(nil)
	probe, err := s.NodeProbe(ctx, n.ID)
	must(err)
	if probe.LocationIPSettings.ManualIP != "198.51.100.20" || probe.Info.Region != "JP" || probe.RegionDetection.IPSource != "manual" {
		t.Fatal(probe)
	}
	nodes, err := s.MonitorNodes(ctx, time.Now(), time.Minute, n.ID)
	must(err)
	if len(nodes) != 1 || nodes[0].Info.Location == nil || nodes[0].Info.Location.Country != "JP" {
		t.Fatal(nodes)
	}
	pub, _ := json.Marshal(nodes[0].Info.PublicInfo())
	for _, secret := range []string{"198.51.100.20", "location_ip", "manual_ip", "ip_source"} {
		if strings.Contains(string(pub), secret) {
			t.Fatalf("private selection leaked: %s", pub)
		}
	}
	set(&LocationIPSettings{Mode: "entry"})
	set(&LocationIPSettings{Mode: "manual", ManualIP: "198.51.100.20"})
	if err = s.SaveRegionDetection(ctx, state, d); !errors.Is(err, ErrRegionObservationChanged) {
		t.Fatal("accepted policy ABA", err)
	}
	current, err := s.NodeRegionState(ctx, n.ID)
	must(err)
	must(s.SaveRegionDetection(ctx, current, d))
	// Explicit manual country and location are independent of the IP source.
	mode := "manual"
	info := NodeProbeInfo{Region: "DE", LocationSettings: &domain.LocationSettings{Mode: "off"}}
	must(s.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, info, 0, 1, "sum", nil, &mode, nil, NodeInfrastructureGuard{}, nil))
	probe, err = s.NodeProbe(ctx, n.ID)
	must(err)
	if probe.Info.Region != "DE" || probe.Info.Location != nil {
		t.Fatal(probe)
	}
	for _, bad := range []LocationIPSettings{{Mode: "other"}, {Mode: "manual"}, {Mode: "manual", ManualIP: "10.10.0.2"}, {Mode: "manual", ManualIP: "example.com"}, {Mode: "manual", ManualIP: "https://example.com"}} {
		if err = s.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, info, 0, 1, "sum", nil, &mode, nil, NodeInfrastructureGuard{}, &bad); !errors.Is(err, ErrLocationIP) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
}
func TestLocationIPFallbackLateResultsAndStableSamples(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	n := &domain.Node{Name: "example"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateNode(ctx, n, "location-ip", time.Hour))
	_, err := s.RedeemPairCode(ctx, "location-ip", "token", "node", "test", "linux")
	must(err)
	at := time.Now().Add(-4 * time.Minute).Truncate(time.Second)
	yes := true
	must(s.setAddressFollowPolicyAt(ctx, n.ID, true, false, &yes, nil, at.Add(-time.Second)))
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(at, "203.0.113.30", false), at))
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(at.Add(2*time.Minute), "203.0.113.30", false), at.Add(2*time.Minute)))
	state, err := s.NodeRegionState(ctx, n.ID)
	must(err)
	if !state.Fresh(time.Now()) || state.Detection.IPSource != "fallback" {
		t.Fatal(state)
	}
	d := RegionDetection{IPv4: RegionResult{IP: "203.0.113.30", Country: "JP", Status: "ok", CheckedAt: time.Now().Unix()}}
	// Ordinary continuation of the same run does not invalidate a lookup.
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", fallbackFollowSample(at.Add(3*time.Minute), "203.0.113.30", false), at.Add(3*time.Minute)))
	must(s.SaveRegionDetection(ctx, state, d))
	// Conflict cancels validity even before reconciliation runs.
	conflict := fallbackFollowSample(at.Add(3*time.Minute+time.Second), "203.0.113.30", false)
	conflict.FallbackIPv4.CheckStatus = "conflict"
	must(s.ObserveNodeAddresses(ctx, n.ID, "token", conflict, at.Add(3*time.Minute+time.Second)))
	if err = s.SaveRegionDetection(ctx, state, d); !errors.Is(err, ErrRegionObservationChanged) {
		t.Fatal("conflict accepted", err)
	}
	p, err := s.NodeProbe(ctx, n.ID)
	must(err)
	if p.RegionDetection.Fresh || p.Info.Region != "" {
		t.Fatal("conflict still located", p)
	}
	// Primary address ABA is also caught, including same IP at both endpoints.
	record := func(ip string, offset time.Duration) {
		t.Helper()
		tm := at.Add(offset)
		must(s.ObserveNodeAddresses(ctx, n.ID, "token", &agentproto.PublicAddresses{SampledAt: tm.Unix(), IPv4: ip}, tm))
	}
	record("198.51.100.20", 181*time.Second)
	state, err = s.NodeRegionState(ctx, n.ID)
	must(err)
	record("192.0.2.10", 182*time.Second)
	record("198.51.100.20", 183*time.Second)
	if err = s.SaveRegionDetection(ctx, state, d); !errors.Is(err, ErrRegionObservationChanged) {
		t.Fatal("address ABA accepted", err)
	}
}

func TestLocationIPLookupSnapshotCannotChangeSourceAsTimePasses(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	input := locationIPFixture(at)
	input.Addresses.Observed.IPv4.LastSuccess = at.Add(-14 * time.Minute).Unix()
	input.Addresses.Observed.IPv6.LastSuccess = at.Add(-14 * time.Minute).Unix()
	snapshot := NodeRegionState{incarnation: "fixture", input: input, selection: input.selectIP(at)}
	if !snapshot.Fresh(at) {
		t.Fatal("initial source unavailable")
	}
	if current := input.selectIP(at.Add(2 * time.Minute)); !current.Fresh || current.Source != "fallback" {
		t.Fatal(current)
	}
	// Both old and new inputs have a usable IP at this time, but the query was
	// started against the primary source and cannot silently adopt fallback.
	if snapshot.Fresh(at.Add(2 * time.Minute)) {
		t.Fatal("old query changed source")
	}
}

func TestLocationIPLateLookupRejectsRevokedAndExpiredEvidence(t *testing.T) {
	for _, change := range []string{"disable_fallback", "disable_follow", "expired", "run_changed", "unpaired"} {
		t.Run(change, func(t *testing.T) {
			s := subDesignStore(t)
			ctx := context.Background()
			n := &domain.Node{Name: "example"}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(s.CreateNode(ctx, n, "location-ip", time.Hour))
			_, err := s.RedeemPairCode(ctx, "location-ip", "token", "node", "test", "linux")
			must(err)
			at := time.Now().Truncate(time.Second)
			input := locationIPFixture(at)
			input.Addresses.Observed.IPv4 = AddressFamily{}
			input.Addresses.Observed.IPv6 = AddressFamily{}
			raw, _ := json.Marshal(input.Addresses.Observed)
			_, err = s.db.Exec(`UPDATE nodes SET address_json=?,address_auto_v4=1,address_fallback_auto_v4=1 WHERE id=?`, string(raw), n.ID)
			must(err)
			state, err := s.NodeRegionState(ctx, n.ID)
			must(err)
			if !state.Fresh(at) {
				t.Fatal(state)
			}
			switch change {
			case "disable_fallback":
				_, err = s.db.Exec(`UPDATE nodes SET address_fallback_auto_v4=0 WHERE id=?`, n.ID)
			case "disable_follow":
				_, err = s.db.Exec(`UPDATE nodes SET address_auto_v4=0 WHERE id=?`, n.ID)
			case "unpaired":
				_, err = s.db.Exec(`UPDATE nodes SET token_hash=NULL WHERE id=?`, n.ID)
			case "run_changed":
				input.Addresses.Observed.FallbackFollowIPv4.RunID = strings.Repeat("b", 32)
				input.Addresses.Observed.FallbackIPv4.RunID = strings.Repeat("b", 32)
				raw, _ = json.Marshal(input.Addresses.Observed)
				_, err = s.db.Exec(`UPDATE nodes SET address_json=? WHERE id=?`, string(raw), n.ID)
			case "expired":
				stale := at.Add(-time.Hour).Unix()
				input.Addresses.Observed.FallbackFollowIPv4.LastSuccess = stale
				input.Addresses.Observed.FallbackFollowIPv4.LastSampledAt = stale
				input.Addresses.Observed.FallbackIPv4.ReceivedAt = stale
				input.Addresses.Observed.FallbackIPv4.SampledAt = stale
				raw, _ = json.Marshal(input.Addresses.Observed)
				_, err = s.db.Exec(`UPDATE nodes SET address_json=? WHERE id=?`, string(raw), n.ID)
			}
			must(err)
			if err = s.SaveRegionDetection(ctx, state, RegionDetection{}); !errors.Is(err, ErrRegionObservationChanged) {
				t.Fatal("late evidence accepted", err)
			}
		})
	}
}

func TestLocationIPRejectsLookupForRecreatedUnpairedNode(t *testing.T) {
	for _, mode := range []string{"manual", "entry"} {
		t.Run(mode, func(t *testing.T) {
			s := subDesignStore(t)
			ctx := context.Background()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			settings := &LocationIPSettings{Mode: mode, ManualIP: "198.51.100.20"}
			create := func() *domain.Node {
				t.Helper()
				n := &domain.Node{Name: "example", PublicAddr: "198.51.100.20"}
				must(s.CreateNode(ctx, n, "location-recreate", time.Hour))
				regionMode := "auto"
				must(s.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, NodeProbeInfo{}, 0, 1, "sum", nil, &regionMode, nil, NodeInfrastructureGuard{}, settings))
				return n
			}
			old := create()
			expected, err := s.NodeRegionState(ctx, old.ID)
			must(err)
			must(s.DeleteNode(ctx, old.ID))
			replacement := create()
			current, err := s.NodeRegionState(ctx, replacement.ID)
			must(err)
			if replacement.ID != old.ID || expected.Token != "" || current.Token != "" || expected.input.Revision != current.input.Revision {
				t.Fatal("fixture did not reproduce identical reusable identity", expected, current)
			}
			if err = s.SaveRegionDetection(ctx, expected, RegionDetection{IPv4: RegionResult{IP: "198.51.100.20", Country: "JP", Status: "ok", CheckedAt: time.Now().Unix()}}); !errors.Is(err, ErrRegionObservationChanged) {
				t.Fatal("accepted deleted node result for replacement", err)
			}
			probe, err := s.NodeProbe(ctx, replacement.ID)
			must(err)
			if probe.Info.Region != "" {
				t.Fatal("replacement inherited old location", probe)
			}
		})
	}
}

func TestLocationIPMissingIncarnationFailsClosed(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	n := &domain.Node{Name: "example", PublicAddr: "198.51.100.20"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateNode(ctx, n, "identity-missing", time.Hour))
	mode := "auto"
	must(s.UpdateNodeProbeWithLocationIP(ctx, n.ID, false, NodeProbeInfo{}, 0, 1, "sum", nil, &mode, nil, NodeInfrastructureGuard{}, &LocationIPSettings{Mode: "entry"}))
	before, err := s.NodeRegionState(ctx, n.ID)
	must(err)
	if !before.Fresh(time.Now()) {
		t.Fatal("valid fixture is not eligible")
	}
	_, err = s.db.Exec(`DELETE FROM probe_public_ids WHERE node_id=?`, n.ID)
	must(err)
	after, err := s.NodeRegionState(ctx, n.ID)
	must(err)
	if after.Fresh(time.Now()) {
		t.Fatal("missing incarnation remains query eligible")
	}
	if err = s.SaveRegionDetection(ctx, before, RegionDetection{}); !errors.Is(err, ErrRegionObservationChanged) {
		t.Fatal("accepted result without current incarnation", err)
	}
}
