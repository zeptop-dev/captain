package store

import (
	"context"
	"errors"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestSiteMapPublicationLifetimeAndAtomicSave(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	e := &domain.Entry{Name: "Published", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	ids, revision, err := s.SiteMapSelection(ctx)
	if err != nil || len(ids) != 0 || revision != 0 {
		t.Fatal(ids, revision, err)
	}
	config := map[string]any{"name": "Old", "map_source": "entries"}
	if _, err = s.SaveSiteWithMap(ctx, config, []int64{e.ID}, nil); !errors.Is(err, ErrMapRevision) {
		t.Fatal("missing revision accepted", err)
	}
	revision, err = s.SaveSiteWithMap(ctx, config, []int64{e.ID}, &revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSiteWithMap(ctx, map[string]any{"name": "Bad"}, []int64{e.ID, 999999}, &revision); !errors.Is(err, ErrMapSelection) {
		t.Fatal(err)
	}
	var saved map[string]any
	if err = s.GetSetting(ctx, "site", &saved); err != nil || saved["name"] != "Old" {
		t.Fatal("partial site save", saved, err)
	}
	if _, err = s.SaveSiteWithMap(ctx, map[string]any{"name": "Legacy"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	ids, _, err = s.SiteMapSelection(ctx)
	if err != nil || len(ids) != 1 || ids[0] != e.ID {
		t.Fatal("legacy save erased selection", ids, err)
	}
	if err = s.SetEntryLocation(ctx, e.ID, domain.LocationSettings{Mode: "manual", Manual: &domain.GeoLocation{Country: "JP", Precision: "country"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	fresh := &domain.Entry{Name: "Unpublished replacement", InboundID: ib.ID, DisplayHost: "replacement.example.com", DisplayPort: 443, Enabled: true}
	if err = s.CreateEntry(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.ID != e.ID {
		t.Fatal("fixture must exercise reusable Entry IDs")
	}
	ids, newRevision, err := s.SiteMapSelection(ctx)
	if err != nil || len(ids) != 0 || newRevision <= revision {
		t.Fatal(ids, newRevision, err)
	}
	if _, err = s.SaveSiteWithMap(ctx, config, []int64{e.ID}, &revision); !errors.Is(err, ErrMapRevision) {
		t.Fatal("stale draft republished replacement", err)
	}
	locations, err := s.EntryLocationSettings(ctx)
	if err != nil || locations[e.ID] != nil {
		t.Fatal("location leaked across Entry identities", locations, err)
	}
}
