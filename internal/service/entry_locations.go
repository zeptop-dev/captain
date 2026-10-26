package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// EntryMapLocations resolves only the Entries already selected by the caller's
// authorization/publication policy. It never publishes infrastructure names,
// addresses or the private detection cache, and never changes Entry.Region.
func EntryMapLocations(ctx context.Context, st *store.Store, entries []*domain.Entry) (map[int64]*domain.GeoLocation, error) {
	out := map[int64]*domain.GeoLocation{}
	if len(entries) == 0 {
		return out, nil
	}
	settings, err := st.EntryLocationSettings(ctx)
	if err != nil {
		return nil, err
	}
	exits, err := st.InboundExitNodes(ctx)
	if err != nil {
		return nil, err
	}
	probes, err := st.ListNodeProbes(ctx)
	if err != nil {
		return nil, err
	}
	routings := map[int64]*store.NodeRouting{}
	overridden := map[int64]bool{}
	for _, entry := range entries {
		out[entry.ID] = nil
		if cfg := settings[entry.ID]; cfg != nil {
			switch cfg.Mode {
			case "off":
				continue
			case "manual":
				out[entry.ID] = cfg.Manual.Clone()
				continue
			}
		}
		inbound, err := st.InboundByID(ctx, entry.InboundID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		exitID := inbound.NodeID
		if reverseExit := exits[inbound.ID]; reverseExit != 0 {
			exitID = reverseExit
		} else {
			// A managed relay line still points at its final proxy inbound.
			// Arbitrary outbound policies, WARP, legacy chains and overrides
			// don't establish one known exit; require a display correction.
			if entry.ChainID != nil {
				continue
			}
			routing, ok := routings[exitID]
			if !ok {
				routing, err = st.NodeRouting(ctx, exitID)
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				if err != nil {
					return nil, err
				}
				routings[exitID] = routing
				overrides, err := st.NodeOverrides(ctx, exitID)
				if err != nil {
					return nil, err
				}
				for _, raw := range overrides {
					if value := strings.TrimSpace(raw); value != "" && value != "{}" {
						overridden[exitID] = true
					}
				}
			}
			if overridden[exitID] || !localMapExit(routing) {
				continue
			}
		}
		if probe := probes[exitID]; probe != nil {
			out[entry.ID] = probe.Info.Location.Clone()
		}
	}
	return out, nil
}

func localMapExit(routing *store.NodeRouting) bool {
	if routing.DefaultOutbound != "" && routing.DefaultOutbound != "direct" {
		return false
	}
	for _, rule := range routing.Routes {
		if rule.Action != "" && rule.Action != "direct" && rule.Action != "block" {
			return false
		}
	}
	return true
}

// MapPoint is a public geographic marker, not a proxy endpoint or a health
// measurement. Country-only points intentionally omit coordinates; the UI can
// show a clearly labelled approximate country centre.
type MapPoint struct {
	Name        string   `json:"name"`
	Country     string   `json:"country"`
	City        string   `json:"city,omitempty"`
	Subdivision string   `json:"subdivision,omitempty"`
	Lat         *float64 `json:"lat,omitempty"`
	Lng         *float64 `json:"lng,omitempty"`
	Precision   string   `json:"precision"`
}

// SiteMapPoints uses an independent, explicitly published Entry allowlist.
// Deleting or disabling an Entry, inbound or client-chain prerequisite removes
// its point; selecting a category never implicitly publishes future members.
func SiteMapPoints(ctx context.Context, st *store.Store) ([]MapPoint, error) {
	st.Topology.Lock()
	defer st.Topology.Unlock()
	ids, _, err := st.SiteMapSelection(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := st.ServiceStatusEntries(ctx, ids, true)
	if err != nil {
		return nil, err
	}
	entries = availableStatusEntries(entries)
	locations, err := EntryMapLocations(ctx, st, entries)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*domain.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	out := []MapPoint{}
	for _, id := range ids {
		e, loc := byID[id], locations[id]
		if e == nil || loc == nil {
			continue
		}
		out = append(out, MapPoint{Name: strings.TrimSpace(e.Name), Country: loc.Country, City: loc.City, Subdivision: loc.Subdivision, Lat: loc.Lat, Lng: loc.Lng, Precision: loc.Precision})
	}
	return out, nil
}
