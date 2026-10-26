package store

import (
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestSiteMapResetStartsWithEmptyPublicationAudit(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: "admin", Status: "active"}
	if err := s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "Previously published", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEntryLocation(ctx, e.ID, domain.LocationSettings{Mode: "manual", Manual: &domain.GeoLocation{Country: "JP", Precision: "country"}}); err != nil {
		t.Fatal(err)
	}
	_, rev, err := s.SiteMapSelection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSiteWithMap(ctx, map[string]any{"name": "Before reset", "map_source": "entries"}, []int64{e.ID}, &rev); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	ids, revision, err := s.SiteMapSelection(ctx)
	if err != nil || len(ids) != 0 || revision != 0 {
		t.Fatal(ids, revision, err)
	}
	locations, err := s.EntryLocationSettings(ctx)
	if err != nil || len(locations) != 0 {
		t.Fatal(locations, err)
	}
	if _, err = s.SaveSiteWithMap(ctx, map[string]any{"name": "After reset", "map_source": "entries"}, []int64{}, &revision); err != nil {
		t.Fatal("first save after reset", err)
	}
}
