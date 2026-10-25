package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestServiceStatusConfigOptInRevisionAndValidation(t *testing.T) {
	s, _, ib, _ := lineFixture(t)
	ctx := context.Background()
	at := time.Now()
	c, err := s.ServiceStatusConfig(ctx)
	if err != nil || c.PublicEnabled || c.Revision != 0 || c.PublicEntryIDs == nil || c.Maintenance == nil {
		t.Fatal(c, err)
	}
	e := &domain.Entry{Name: "Example node", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	c.PublicEnabled = true
	c.PublicEntryIDs = []int64{e.ID}
	c.Maintenance = []ServiceMaintenance{{EntryID: e.ID, StartsAt: at.Add(-time.Minute).Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: " Planned work "}}
	c, err = s.SaveServiceStatusConfig(ctx, c, at)
	if err != nil || c.Revision != 1 || c.Maintenance[0].Note != "Planned work" {
		t.Fatal(c, err)
	}
	stale := c
	stale.Revision = 0
	stale.PublicEnabled = false
	if _, err := s.SaveServiceStatusConfig(ctx, stale, at); !errors.Is(err, ErrServiceStatusConflict) {
		t.Fatal(err)
	}
	cases := []ServiceStatusConfig{}
	bad := c
	bad.PublicEntryIDs = []int64{e.ID, 999999}
	cases = append(cases, bad)
	bad = c
	bad.PublicEntryIDs = []int64{e.ID, e.ID}
	cases = append(cases, bad)
	for _, m := range []ServiceMaintenance{
		{EntryID: 999999, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix()},
		{EntryID: e.ID, StartsAt: at.Unix(), EndsAt: at.Unix()},
		{EntryID: e.ID, StartsAt: at.Unix(), EndsAt: at.Add(31 * 24 * time.Hour).Unix()},
		{EntryID: e.ID, StartsAt: at.Add(366 * 24 * time.Hour).Unix(), EndsAt: at.Add(367 * 24 * time.Hour).Unix()},
		{EntryID: e.ID, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: "invalid\x00note"},
		{EntryID: e.ID, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: strings.Repeat("x", 513)},
	} {
		bad = c
		bad.Maintenance = []ServiceMaintenance{m}
		cases = append(cases, bad)
	}
	for _, bad := range cases {
		if _, err := s.SaveServiceStatusConfig(ctx, bad, at); !errors.Is(err, ErrServiceStatusInvalid) {
			t.Fatalf("accepted invalid settings: %+v %v", bad, err)
		}
		got, err := s.ServiceStatusConfig(ctx)
		if err != nil || !reflect.DeepEqual(c, got) {
			t.Fatal("partial save changed publication", got, err)
		}
	}
	// Selecting a category never implicitly publishes future members.
	e2 := &domain.Entry{Name: "New node", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, e2); err != nil {
		t.Fatal(err)
	}
	got, err := s.ServiceStatusConfig(ctx)
	if err != nil || len(got.PublicEntryIDs) != 1 || got.PublicEntryIDs[0] != e.ID {
		t.Fatal(got, err)
	}
	before, err := s.EntryByID(ctx, e.ID)
	if err != nil || !before.Enabled || before.InboundID != ib.ID {
		t.Fatal("maintenance changed routing", before, err)
	}
}

func TestServiceStatusConfigCannotReplaceExistingProbePath(t *testing.T) {
	s, _, _, _ := lineFixture(t)
	ctx := context.Background()
	if err := s.SetSetting(ctx, SettingProbe, ProbeSettings{Enabled: true, Path: ServiceStatusPath}); err != nil {
		t.Fatal(err)
	}
	_, err := s.SaveServiceStatusConfig(ctx, ServiceStatusConfig{PublicEnabled: true}, time.Now())
	if !errors.Is(err, ErrServiceStatusPathConflict) {
		t.Fatal(err)
	}
	got, err := s.ServiceStatusConfig(ctx)
	if err != nil || got.PublicEnabled || got.Revision != 0 {
		t.Fatal("failed enable changed settings", got, err)
	}
}
