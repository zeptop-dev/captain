package service

import (
	"reflect"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func mapCity(country, city string, lat, lng float64) *domain.GeoLocation {
	return &domain.GeoLocation{Country: country, City: city, Lat: &lat, Lng: &lng, Precision: "city"}
}

func setMapNode(t *testing.T, r *runtimeRig, node int64, loc *domain.GeoLocation) {
	t.Helper()
	info := store.NodeProbeInfo{LocationSettings: &domain.LocationSettings{Mode: "manual", Manual: loc}}
	if err := r.store.UpdateNodeProbe(r.ctx, node, false, info, 0, 1, "sum"); err != nil {
		t.Fatal(err)
	}
}

func TestEntryMapLocationFollowsExitAndPreservesMetadata(t *testing.T) {
	for _, kind := range []string{"wireguard", "reverse"} {
		t.Run(kind, func(t *testing.T) {
			r := newRuntimeRig(t)
			a, b := r.node("front", "192.0.2.10"), r.node("exit", "203.0.113.30")
			front, exit := mapCity("JP", "Tokyo", 35.68, 139.69), mapCity("US", "Los Angeles", 34.05, -118.24)
			setMapNode(t, r, a.ID, front)
			setMapNode(t, r, b.ID, exit)
			ib := domain.Inbound{NodeID: a.ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true}
			e := domain.Entry{Name: "Residential", DisplayHost: "example.com", DisplayPort: 18443, Enabled: true, Region: "US"}
			var entryID, inboundID int64
			var edit func() error
			if kind == "wireguard" {
				w := store.WireGuardWrite{Link: store.WireGuardLink{ID: "geo-wg", TransitID: a.ID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380}, User: ib, Entry: e}
				if err := r.store.SaveWireGuardLink(r.ctx, &w); err != nil {
					t.Fatal(err)
				}
				entryID, inboundID = w.Entry.ID, w.User.ID
				edit = func() error { w.Entry.Name = "Renamed"; return r.store.SaveWireGuardLink(r.ctx, &w) }
			} else {
				receiver := ib
				receiver.Tag, receiver.Port = "receiver", 19443
				writes := []store.ReverseWrite{{Link: store.ReverseLink{ID: "geo-reverse", TransitID: a.ID, ExitID: b.ID}, User: ib, Receiver: receiver, Entry: e}}
				if err := r.store.SaveReverseLinks(r.ctx, b.ID, nil, writes); err != nil {
					t.Fatal(err)
				}
				entryID, inboundID = writes[0].Entry.ID, writes[0].User.ID
				edit = func() error {
					writes[0].Link.Version = 1
					writes[0].Entry.Name = "Renamed"
					return r.store.SaveReverseLinks(r.ctx, b.ID, map[string]int64{"geo-reverse": 1}, writes)
				}
			}
			original, err := r.store.EntryByID(r.ctx, entryID)
			if err != nil {
				t.Fatal(err)
			}
			alias := &domain.Entry{Name: "Alias", InboundID: inboundID, DisplayHost: "alias.example.com", DisplayPort: 443, Enabled: true}
			if err = r.store.CreateEntry(r.ctx, alias); err != nil {
				t.Fatal(err)
			}
			got, err := EntryMapLocations(r.ctx, r.store, []*domain.Entry{original, alias})
			if err != nil || !reflect.DeepEqual(got[entryID], exit) || !reflect.DeepEqual(got[alias.ID], exit) {
				t.Fatalf("front incorrectly used for exit: %+v %v", got, err)
			}
			manual := mapCity("US", "New York", 40.71, -74.01)
			if err = r.store.SetEntryLocation(r.ctx, entryID, domain.LocationSettings{Mode: "manual", Manual: manual}); err != nil {
				t.Fatal(err)
			}
			if err = edit(); err != nil {
				t.Fatal(err)
			}
			got, err = EntryMapLocations(r.ctx, r.store, []*domain.Entry{original, alias})
			if err != nil || !reflect.DeepEqual(got[entryID], manual) || !reflect.DeepEqual(got[alias.ID], exit) {
				t.Fatal("managed edit erased override", got, err)
			}
			for _, mode := range []string{"off", "auto"} {
				if err = r.store.SetEntryLocation(r.ctx, entryID, domain.LocationSettings{Mode: mode, Manual: manual}); err != nil {
					t.Fatal(err)
				}
				got, err = EntryMapLocations(r.ctx, r.store, []*domain.Entry{original})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "off" && got[entryID] != nil {
					t.Fatal("hidden override exposed")
				}
				if mode == "auto" && !reflect.DeepEqual(got[entryID], exit) {
					t.Fatal("inheritance failed")
				}
			}
		})
	}
}

func TestEntryMapDoesNotInventExitForRemotePolicy(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("server", "203.0.113.30")
	ib := r.inbound(n, "ordinary")
	e := r.entry(n, ib)
	loc := mapCity("US", "New York", 40.71, -74.01)
	setMapNode(t, r, n.ID, loc)
	got, err := EntryMapLocations(r.ctx, r.store, []*domain.Entry{e})
	if err != nil || !reflect.DeepEqual(got[e.ID], loc) {
		t.Fatal(got, err)
	}
	for _, nr := range []*store.NodeRouting{{DefaultOutbound: "warp"}, {Routes: []spec.RouteRule{{Action: "outbound", Value: "remote"}}}} {
		if err = r.store.SetNodeRouting(r.ctx, n.ID, nr); err != nil {
			t.Fatal(err)
		}
		got, err = EntryMapLocations(r.ctx, r.store, []*domain.Entry{e})
		if err != nil || got[e.ID] != nil {
			t.Fatal("ambiguous exit placed at inbound", got, err)
		}
	}
	if err = r.store.SetNodeRouting(r.ctx, n.ID, &store.NodeRouting{}); err != nil {
		t.Fatal(err)
	}
	if err = r.store.SetNodeOverrides(r.ctx, n.ID, map[string]string{"xray": `{"routing":{"domainStrategy":"AsIs"}}`}); err != nil {
		t.Fatal(err)
	}
	got, err = EntryMapLocations(r.ctx, r.store, []*domain.Entry{e})
	if err != nil || got[e.ID] != nil {
		t.Fatal("raw override claimed a known exit", got, err)
	}
}
