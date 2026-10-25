package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
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

func TestServiceStatusEntryRetirementAndIDReuse(t *testing.T) {
	for _, path := range []string{"entry", "inbound", "server", "wireguard", "reverse", "wireguard_exit", "reverse_exit"} {
		t.Run(path, func(t *testing.T) {
			s, ns, ib, _ := lineFixture(t)
			ctx := context.Background()
			at := time.Now()
			keepInbound := &domain.Inbound{NodeID: ns[0].ID, Tag: "survivor", Protocol: spec.VLESS, Port: 9443, Enabled: true}
			if err := s.CreateInbound(ctx, keepInbound); err != nil {
				t.Fatal(err)
			}
			keep := &domain.Entry{Name: "Surviving node", InboundID: keepInbound.ID, DisplayHost: "example.com", DisplayPort: 9443, Enabled: true}
			if err := s.CreateEntry(ctx, keep); err != nil {
				t.Fatal(err)
			}
			var retiredID int64
			var remove func() error
			switch path {
			case "entry", "inbound", "server":
				e := &domain.Entry{Name: "Retired node", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
				if err := s.CreateEntry(ctx, e); err != nil {
					t.Fatal(err)
				}
				retiredID = e.ID
				switch path {
				case "entry":
					remove = func() error { return s.DeleteEntry(ctx, e.ID) }
				case "inbound":
					remove = func() error { return s.DeleteInbound(ctx, ib.ID) }
				case "server":
					remove = func() error { _, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, ib.NodeID); return err }
				}
			case "wireguard", "wireguard_exit":
				w := WireGuardWrite{
					Link:  WireGuardLink{ID: "example-wg", TransitID: ns[1].ID, ExitID: ns[2].ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380},
					User:  domain.Inbound{NodeID: ns[1].ID, Tag: "example-wg", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true},
					Entry: domain.Entry{Name: "Retired WG node", DisplayHost: "example.com", DisplayPort: 18443, Enabled: true},
				}
				if err := s.SaveWireGuardLink(ctx, &w); err != nil {
					t.Fatal(err)
				}
				retiredID = w.Entry.ID
				if path == "wireguard" {
					remove = func() error { return s.DeleteWireGuardLink(ctx, w.Link.ExitID, w.Link.ID, w.Link.Version) }
				} else {
					remove = func() error {
						_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, w.Link.ExitID)
						return err
					}
				}
			case "reverse", "reverse_exit":
				writes := []ReverseWrite{{
					Link:     ReverseLink{ID: "example-reverse", TransitID: ns[1].ID},
					User:     domain.Inbound{NodeID: ns[1].ID, Tag: "example-reverse", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true},
					Receiver: domain.Inbound{NodeID: ns[1].ID, Tag: "example-receiver", Protocol: spec.VLESS, Port: 19443, Core: "xray", Enabled: true},
					Entry:    domain.Entry{Name: "Retired reverse node", DisplayHost: "example.com", DisplayPort: 18443, Enabled: true},
				}}
				if err := s.SaveReverseLinks(ctx, ns[2].ID, nil, writes); err != nil {
					t.Fatal(err)
				}
				retiredID = writes[0].Entry.ID
				if path == "reverse" {
					remove = func() error { return s.SaveReverseLinks(ctx, ns[2].ID, map[string]int64{"example-reverse": 1}, nil) }
				} else {
					remove = func() error { _, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, ns[2].ID); return err }
				}
			}
			old, err := s.SaveServiceStatusConfig(ctx, ServiceStatusConfig{
				PublicEnabled: true, PublicEntryIDs: []int64{retiredID, keep.ID},
				Maintenance: []ServiceMaintenance{
					{EntryID: retiredID, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: "Retired maintenance"},
					{EntryID: keep.ID, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: "Keep maintenance"},
				},
			}, at)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := s.ServiceStatusConfig(ctx); err != nil || !reflect.DeepEqual(got, old) {
				t.Fatal("configuration order did not round trip", got, err)
			}
			if err := remove(); err != nil {
				t.Fatal(err)
			}
			got, err := s.ServiceStatusConfig(ctx)
			if err != nil || !got.PublicEnabled || got.Revision != old.Revision+1 || !reflect.DeepEqual(got.PublicEntryIDs, []int64{keep.ID}) || !reflect.DeepEqual(got.Maintenance, old.Maintenance[1:]) {
				t.Fatal("retired entry kept publication or maintenance", got, err)
			}
			// Even a newly loaded form cannot retain a missing reference.
			invalid := old
			invalid.Revision = got.Revision
			if _, err := s.SaveServiceStatusConfig(ctx, invalid, at); !errors.Is(err, ErrServiceStatusInvalid) {
				t.Fatal("accepted missing entry", err)
			}
			replacement := &domain.Entry{Name: "Unselected replacement", InboundID: keepInbound.ID, DisplayHost: "example.com", DisplayPort: 9443, Enabled: true}
			if err := s.CreateEntry(ctx, replacement); err != nil || replacement.ID != retiredID {
				t.Fatal("fixture must reuse the retired SQLite row ID", replacement, err)
			}
			if after, err := s.ServiceStatusConfig(ctx); err != nil || !reflect.DeepEqual(got, after) {
				t.Fatal("new entry inherited old publication", after, err)
			}
			if _, err := s.SaveServiceStatusConfig(ctx, old, at); !errors.Is(err, ErrServiceStatusConflict) {
				t.Fatal("old draft rebound a reused ID", err)
			}
			// Selecting the new identity after reloading is an intentional publish.
			got.PublicEntryIDs = append(got.PublicEntryIDs, replacement.ID)
			if published, err := s.SaveServiceStatusConfig(ctx, got, at); err != nil || published.Revision != got.Revision+1 || len(published.Maintenance) != 1 {
				t.Fatal("fresh selection could not be published", published, err)
			}
		})
	}
}

func TestServiceStatusUnpublishedRetirementInvalidatesFirstDraft(t *testing.T) {
	s, _, ib, _ := lineFixture(t)
	ctx := context.Background()
	old, err := s.ServiceStatusConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "Only in unsaved draft", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	old.PublicEnabled = true
	old.PublicEntryIDs = []int64{e.ID}
	if err := s.DeleteEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	replacement := &domain.Entry{Name: "New private entry", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
	if err := s.CreateEntry(ctx, replacement); err != nil || replacement.ID != e.ID {
		t.Fatal(replacement, err)
	}
	if _, err := s.SaveServiceStatusConfig(ctx, old, time.Now()); !errors.Is(err, ErrServiceStatusConflict) {
		t.Fatal("first unsaved draft survived entry identity change", err)
	}
	got, err := s.ServiceStatusConfig(ctx)
	if err != nil || got.Revision != 1 || got.PublicEnabled || len(got.PublicEntryIDs) != 0 || len(got.Maintenance) != 0 {
		t.Fatal(got, err)
	}
}

func TestServiceStatusConfigConsistentSnapshot(t *testing.T) {
	s, _, ib, _ := lineFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	at := time.Now()
	ids := make([]int64, 2)
	for i := range ids {
		e := &domain.Entry{Name: fmt.Sprintf("Snapshot %d", i), InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true}
		if err := s.CreateEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
		ids[i] = e.ID
	}
	config := func(revision int64) ServiceStatusConfig {
		id := ids[revision%2]
		return ServiceStatusConfig{Revision: revision, PublicEnabled: revision%2 == 0, PublicEntryIDs: []int64{id}, Maintenance: []ServiceMaintenance{{EntryID: id, StartsAt: at.Unix(), EndsAt: at.Add(time.Hour).Unix(), Note: fmt.Sprint(revision)}}}
	}
	if _, err := s.SaveServiceStatusConfig(ctx, config(0), at); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for revision := int64(1); revision < 40; revision++ {
			if _, err := s.SaveServiceStatusConfig(ctx, config(revision), at); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		got, err := s.ServiceStatusConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := config(got.Revision - 1)
		want.Revision++
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mixed configuration generations: got %+v, want %+v", got, want)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
