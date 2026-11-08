package store

import (
	"context"
	"errors"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestEntryIPVersionPersistenceAndValidation(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	e := &domain.Entry{Name: "Dual node", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, Enabled: true, IPVersion: "dual"}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.EntryByID(ctx, e.ID)
	if err != nil || got.IPVersion != "dual" {
		t.Fatal(got, err)
	}
	u := &domain.User{Email: "ip-policy@example.com", UUID: "ip-policy-user", Status: "active"}
	if err = s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	rows, err := s.EntriesForUser(ctx, u)
	if err != nil || len(rows) != 1 || rows[0].Entry.IPVersion != "dual" {
		t.Fatal(rows, err)
	}
	for _, tc := range []struct {
		host, mode string
		extra      map[string]any
	}{
		{"node.example.com", "invalid", nil}, {"192.0.2.10", "ipv6", nil}, {"2001:db8::10", "ipv4", nil}, {"192.0.2.10", "dual", nil},
		{"node.example.com", "ipv4", map[string]any{"ip-version": "ipv6"}}, {"node.example.com", "dual", map[string]any{"server": "other.example.com"}},
		{"node.example.com", "dual", map[string]any{"dialer-proxy": "front"}},
	} {
		bad := *e
		bad.DisplayHost, bad.IPVersion, bad.ClientExtra = tc.host, tc.mode, tc.extra
		if err := s.UpdateEntry(ctx, &bad); !errors.Is(err, ErrEntryIPVersion) {
			t.Fatalf("accepted %+v: %v", tc, err)
		}
	}
	got, err = s.EntryByID(ctx, e.ID)
	if err != nil || got.IPVersion != "dual" || got.DisplayHost != "node.example.com" {
		t.Fatal("failed edit changed state", got, err)
	}
	legacy := *e
	legacy.ID = 0
	legacy.IPVersion = ""
	legacy.ClientExtra = map[string]any{"ip-version": "ipv6"}
	if err = s.CreateEntry(ctx, &legacy); err != nil {
		t.Fatal("legacy extra rejected", err)
	}
	chained := *e
	chained.ID = 0
	chained.ClientDialerEntryID = &legacy.ID
	if err = s.CreateEntry(ctx, &chained); !errors.Is(err, ErrEntryIPVersion) {
		t.Fatal("client chain accepted", err)
	}
}

func TestEntryIPVariantsAtomicAccessAndRollback(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	cat, err := s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	base := &domain.Entry{Name: "Example", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, Rate: 2, Sort: 7, Tags: []string{"custom"}, Region: "JP", Enabled: true, Access: &domain.InboundAccessChange{InboundID: ib.ID, Policy: domain.AccessNone, Revision: cat.Revision}}
	if _, err = s.CreateEntryVariants(ctx, base); !errors.Is(err, ErrAccessConfirmationRequired) {
		t.Fatal("shared impact not checked", err)
	}
	base.Access.ConfirmShared = true
	if _, err = s.db.Exec(`CREATE TRIGGER reject_second_variant BEFORE INSERT ON entries WHEN NEW.ip_version='ipv6' BEGIN SELECT RAISE(ABORT,'test insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateEntryVariants(ctx, base); err == nil {
		t.Fatal("second insertion failure hidden")
	}
	entries, err := s.ListEntries(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatal("partial entries", entries, err)
	}
	after, err := s.AccessCatalog(ctx)
	if err != nil || after.Revision != cat.Revision {
		t.Fatal("partial access edit survived", after, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_second_variant`); err != nil {
		t.Fatal(err)
	}
	entries, err = s.CreateEntryVariants(ctx, base)
	if err != nil || len(entries) != 3 {
		t.Fatal(entries, err)
	}
	for i, mode := range []string{"ipv4", "ipv6", "dual"} {
		if e := entries[i]; e.IPVersion != mode || e.InboundID != ib.ID || e.Rate != 2 || e.Sort != 7 || len(e.Tags) != 1 || e.Region != "JP" {
			t.Fatal("variant lost metadata", e)
		}
	}
	access, err := s.InboundAccess(ctx, ib.ID)
	if err != nil || access.Policy != domain.AccessNone {
		t.Fatal(access, err)
	}
	if _, err = s.CreateEntryVariants(ctx, base); !errors.Is(err, ErrAccessConflict) {
		t.Fatal("stale access revision accepted", err)
	}
}

func TestEntryIPVersionLineAndTopologyPreservePolicy(t *testing.T) {
	ctx := context.Background()
	s, _, ib, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "IPv4 route", LineID: &l.ID, IPVersion: "ipv4", Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].Port++
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, err := s.EntryByID(ctx, e.ID)
	if err != nil || got.IPVersion != "ipv4" {
		t.Fatal(got, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = linePublishedMembersTx(ctx, tx, l.ID, l.TargetInboundID, map[string]LineEndpoint{e.LineMemberID: {Host: "2001:db8::10", Port: 18443}})
	_ = tx.Rollback()
	if !errors.Is(err, ErrEntryIPVersion) {
		t.Fatal("incompatible generated endpoint accepted", err)
	}
	direct := &domain.Entry{Name: "Editable", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, Enabled: true, IPVersion: "ipv6"}
	if err = s.CreateEntry(ctx, direct); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, entry := range catalog.Entries {
		if entry.ID == direct.ID {
			revision = entry.Revision
			if entry.IPVersion != "ipv6" {
				t.Fatal(entry)
			}
		}
	}
	draft := &TopologyChangeset{Name: "Rename", Operations: []TopologyOperation{{Key: "entry", Kind: "entry", ID: direct.ID, Revision: revision, Entry: &TopologyEntryPatch{Name: topologyPtr("Renamed")}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, draft.ID, draft.Version, preview.Revision); err != nil {
		t.Fatal(err)
	}
	got, err = s.EntryByID(ctx, direct.ID)
	if err != nil || got.IPVersion != "ipv6" || got.Name != "Renamed" {
		t.Fatal(got, err)
	}
}

func TestEntryIPVersionWireGuardHostnameIsRejected(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	ib.Protocol = spec.WireGuard
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ipv4", "ipv6"} {
		e := &domain.Entry{Name: "WG", InboundID: ib.ID, DisplayHost: "wg.example.com", DisplayPort: 51820, IPVersion: mode, Enabled: true}
		if err := s.CreateEntry(ctx, e); !errors.Is(err, ErrEntryIPVersion) {
			t.Fatalf("unsupported WG hostname policy %s saved: %v", mode, err)
		}
	}
	for _, tc := range []struct{ host, mode string }{{"wg.example.com", ""}, {"wg.example.com", "dual"}, {"192.0.2.20", "ipv4"}, {"[2001:db8::20]", "ipv6"}} {
		e := &domain.Entry{Name: "WG", InboundID: ib.ID, DisplayHost: tc.host, DisplayPort: 51820, IPVersion: tc.mode, Enabled: true}
		if err := s.CreateEntry(ctx, e); err != nil {
			t.Fatalf("valid WG policy %+v: %v", tc, err)
		}
		if tc.mode == "ipv4" || tc.mode == "ipv6" {
			e.DisplayHost = "wg.example.com"
			if err := s.UpdateEntry(ctx, e); !errors.Is(err, ErrEntryIPVersion) {
				t.Fatalf("endpoint edit bypassed protocol check: %v", err)
			}
			saved, err := s.EntryByID(ctx, e.ID)
			if err != nil || saved.DisplayHost != tc.host {
				t.Fatal("failed edit wrote host", saved, err)
			}
		}
	}
	cat, err := s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	base := &domain.Entry{Name: "WG", InboundID: ib.ID, DisplayHost: "wg.example.com", DisplayPort: 51820, Enabled: true, Access: &domain.InboundAccessChange{InboundID: ib.ID, Policy: domain.AccessNone, Revision: cat.Revision, ConfirmShared: true}}
	if _, err = s.CreateEntryVariants(ctx, base); !errors.Is(err, ErrEntryIPVersion) {
		t.Fatalf("WG variants accepted: %v", err)
	}
	after, err := s.AccessCatalog(ctx)
	if err != nil || after.Revision != cat.Revision {
		t.Fatal("failed batch changed access", after, err)
	}
	entries, err := s.ListEntries(ctx)
	if err != nil || len(entries) != 4 {
		t.Fatal("failed batch created entries", len(entries), err)
	}
	// Draft preview uses the final inbound protocol, including referenced
	// existing inbounds; commit cannot be used to bypass single-entry validation.
	draft := &TopologyChangeset{Name: "WG family", Operations: []TopologyOperation{{Key: "entry", Kind: "entry", Entry: &TopologyEntryPatch{Name: topologyPtr("WG"), InboundID: &ib.ID, DisplayHost: topologyPtr("wg.example.com"), DisplayPort: topologyPtr(51820), IPVersion: topologyPtr("ipv4")}}}}
	if err = s.SaveTopologyChangeset(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewTopologyChangeset(ctx, draft.ID, draft.Version); !errors.Is(err, ErrEntryIPVersion) {
		t.Fatal("draft accepted unsupported WG policy", err)
	}
}

func TestEntryIPVersionInboundProtocolAndLineTargetGuard(t *testing.T) {
	ctx := context.Background()
	s, _, ib, l := lineFixture(t)
	e := &domain.Entry{Name: "IPv4", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, IPVersion: "ipv4", Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	ib.Protocol = spec.WireGuard
	if err := s.UpdateInbound(ctx, ib); !errors.Is(err, ErrEntryIPVersion) {
		t.Fatal("inbound protocol edit bypassed policy", err)
	}
	saved, err := s.InboundByID(ctx, ib.ID)
	if err != nil || saved.Protocol != spec.VLESS {
		t.Fatal("failed protocol edit persisted", saved, err)
	}
	if err = s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	bound := &domain.Entry{Name: "Route IPv4", LineID: &l.ID, IPVersion: "ipv4", Enabled: true}
	if err = s.CreateEntry(ctx, bound); err != nil {
		t.Fatal(err)
	}
	wgInbound := *ib
	wgInbound.ID, wgInbound.Tag, wgInbound.Port = 0, "wg", 51820
	if err = s.CreateInbound(ctx, &wgInbound); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The existing entry still points at VLESS, but the proposed line points at
	// WireGuard. Validation must use that new target rather than its stored one.
	_, err = linePublishedMembersTx(ctx, tx, l.ID, wgInbound.ID, map[string]LineEndpoint{bound.LineMemberID: {Host: "relay.example.com", Port: 18443}})
	_ = tx.Rollback()
	if !errors.Is(err, ErrEntryIPVersion) {
		t.Fatal("new line target protocol ignored", err)
	}
}
