package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func topologyPtr[T any](v T) *T { return &v }
func topologyDraftFixture(t *testing.T) (*Store, *TopologyChangeset) {
	t.Helper()
	s, ns, _, l := lineFixture(t)
	d := &TopologyChangeset{Name: "New subscription route", Operations: []TopologyOperation{
		{Key: "landing", Kind: "inbound", Inbound: &TopologyInboundPatch{NodeID: ns[3].ID, Tag: topologyPtr("new-landing"), Port: topologyPtr(2443), Protocol: spec.Shadowsocks, Settings: &spec.Inbound{Cipher: "2022-blake3-aes-128-gcm"}}},
		{Key: "route", Kind: "line", InboundRef: "landing", Line: l},
		{Key: "offer", Kind: "entry", Entry: &TopologyEntryPatch{Name: topologyPtr("New offer"), LineRef: "route"}},
	}}
	d.Operations[1].Line.Protocol = "both"
	if err := s.SaveTopologyChangeset(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return s, d
}
func TestTopologyChangesetAtomicReferencesAndPrivateSettings(t *testing.T) {
	ctx := context.Background()
	s, d := topologyDraftFixture(t)
	var private string
	if err := s.db.QueryRow(`SELECT operations_json FROM topology_changesets WHERE id=?`, d.ID).Scan(&private); err != nil {
		t.Fatal(err)
	}
	var ops []TopologyOperation
	if err := json.Unmarshal([]byte(private), &ops); err != nil {
		t.Fatal(err)
	}
	secret := ops[0].Inbound.Settings.ServerKey
	if secret == "" {
		t.Fatal("creation secret missing")
	}
	public, _ := json.Marshal(d)
	if strings.Contains(string(public), secret) || d.Operations[0].Inbound.Settings != nil {
		t.Fatal("draft leaked settings")
	}
	// Query-only enforces that preview truly performs no writes, including temp IDs.
	if _, err := s.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if _, e := s.db.Exec(`PRAGMA query_only=OFF`); e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil || p.Revision != q.Revision {
		t.Fatal("unstable preview", err)
	}
	result, err := s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, r := range result.Result {
		ids[r.Key] = r.ID
	}
	ib, err := s.InboundByID(ctx, ids["landing"])
	if err != nil || ib.Settings.ServerKey != secret {
		t.Fatal("creation settings changed", err)
	}
	entry, err := s.EntryByID(ctx, ids["offer"])
	if err != nil || entry.InboundID != ib.ID || entry.LineID == nil || *entry.LineID != ids["route"] || entry.DisplayPort != 18443 {
		t.Fatal("references not atomically resolved", entry, err)
	}
	if err := s.db.QueryRow(`SELECT operations_json FROM topology_changesets WHERE id=?`, d.ID).Scan(&private); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(private, secret) {
		t.Fatal("committed history retained creation secret")
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); !errors.Is(err, ErrTopologyConflict) {
		t.Fatal("duplicate commit", err)
	}
}
func TestTopologyChangesetRollbackAndStalePreview(t *testing.T) {
	ctx := context.Background()
	s, d := topologyDraftFixture(t)
	p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE nodes SET token_hash='rotated' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); !errors.Is(err, ErrTopologyConflict) {
		t.Fatal("token rotation accepted stale preview", err)
	}
	p, err = s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER topology_fail BEFORE INSERT ON line_changes BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); err == nil {
		t.Fatal("injected failure ignored")
	}
	for table, want := range map[string]int{"inbounds": 1, "forward_lines": 0, "entries": 0} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("partial %s write: %d %v", table, n, err)
		}
	}
	got, err := s.TopologyChangeset(ctx, d.ID)
	if err != nil || got.Status != "draft" {
		t.Fatal("failed commit lost draft", err)
	}
}
func TestTopologyChangesetEditPreservesSecretsAndACL(t *testing.T) {
	ctx := context.Background()
	s, _, ib, _ := lineFixture(t)
	ib.Settings.ServerKey = "keep-me"
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := &TopologyChangeset{Name: "Metadata", Operations: []TopologyOperation{{Key: "edit", Kind: "inbound", ID: ib.ID, Revision: catalog.Inbounds[0].Revision, Inbound: &TopologyInboundPatch{Tag: topologyPtr("renamed")}}}}
	if err = s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); err != nil {
		t.Fatal(err)
	}
	got, err := s.InboundByID(ctx, ib.ID)
	if err != nil || got.Settings.ServerKey != "keep-me" || got.Port != ib.Port || got.Tag != "renamed" {
		t.Fatal("omitted fields lost", got, err)
	}
}

func TestTopologyChangesetUniqueSwapAndDraftCAS(t *testing.T) {
	ctx := context.Background()
	s, ns, first, _ := lineFixture(t)
	second := &domain.Inbound{NodeID: ns[3].ID, Tag: "second", Port: 444, Protocol: spec.VLESS, Enabled: true}
	if err := s.CreateInbound(ctx, second); err != nil {
		t.Fatal(err)
	}
	c, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rev := map[int64]string{}
	for _, v := range c.Inbounds {
		rev[v.ID] = v.Revision
	}
	d := &TopologyChangeset{Name: "Swap listeners", Operations: []TopologyOperation{
		{Key: "one", Kind: "inbound", ID: first.ID, Revision: rev[first.ID], Inbound: &TopologyInboundPatch{Tag: topologyPtr(second.Tag), Port: topologyPtr(second.Port)}},
		{Key: "two", Kind: "inbound", ID: second.ID, Revision: rev[second.ID], Inbound: &TopologyInboundPatch{Tag: topologyPtr(first.Tag), Port: topologyPtr(first.Port)}},
	}}
	if err = s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	stale := topologyCopy(*d)
	d.Name = "Renamed draft"
	if err = s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTopologyChangeset(ctx, &stale); !errors.Is(err, ErrTopologyConflict) {
		t.Fatal("stale save", err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, stale.Version, p.Revision); !errors.Is(err, ErrTopologyConflict) {
		t.Fatal("stale commit", err)
	}
	p, err = s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); err != nil {
		t.Fatal(err)
	}
	a, _ := s.InboundByID(ctx, first.ID)
	b, _ := s.InboundByID(ctx, second.ID)
	if a.Tag != second.Tag || a.Port != second.Port || b.Tag != first.Tag || b.Port != first.Port {
		t.Fatal("swap not committed", a, b)
	}
}
func TestTopologyChangesetObjectIncarnationRejectsABA(t *testing.T) {
	for _, kind := range []string{"inbound", "entry"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, _, ib, _ := lineFixture(t)
			e := &domain.Entry{Name: "Existing", InboundID: ib.ID, DisplayHost: "192.0.2.1", DisplayPort: 443, Enabled: true}
			if err := s.CreateEntry(ctx, e); err != nil {
				t.Fatal(err)
			}
			c, err := s.TopologyChangesetCatalog(ctx)
			if err != nil {
				t.Fatal(err)
			}
			op := TopologyOperation{Key: "edit", Kind: kind}
			table, col := "inbounds", "inbound_id"
			if kind == "inbound" {
				op.ID = ib.ID
				op.Revision = c.Inbounds[0].Revision
				op.Inbound = &TopologyInboundPatch{Tag: topologyPtr("new-name")}
			} else {
				op.ID = e.ID
				op.Revision = c.Entries[0].Revision
				op.Entry = &TopologyEntryPatch{Name: topologyPtr("New title")}
				table, col = "entries", "entry_id"
			}
			d := &TopologyChangeset{Name: "ABA guard", Operations: []TopologyOperation{op}}
			if err = s.SaveTopologyChangeset(ctx, d); err != nil {
				t.Fatal(err)
			}
			p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve every resource column, including timestamps, then force the same
			// integer ID. Only the insertion identity may distinguish these objects.
			if _, err = s.db.Exec("CREATE TEMP TABLE old_topology_object AS SELECT * FROM "+table+" WHERE id=?", op.ID); err != nil {
				t.Fatal(err)
			}
			var before, after string
			identityTable := "topology_inbound_identities"
			if kind == "entry" {
				identityTable = "topology_entry_identities"
			}
			if err = s.db.QueryRow("SELECT identity FROM "+identityTable+" WHERE "+col+"=?", op.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("DELETE FROM "+table+" WHERE id=?", op.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("INSERT INTO " + table + " SELECT * FROM old_topology_object"); err != nil {
				t.Fatal(err)
			}
			if err = s.db.QueryRow("SELECT identity FROM "+identityTable+" WHERE "+col+"=?", op.ID).Scan(&after); err != nil || before == after {
				t.Fatal("identity reused", err)
			}
			if _, err = s.PreviewTopologyChangeset(ctx, d.ID, d.Version); !errors.Is(err, ErrTopologyConflict) {
				t.Fatal("ABA preview accepted", err)
			}
			if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); !errors.Is(err, ErrTopologyConflict) {
				t.Fatal("ABA commit accepted", err)
			}
		})
	}
}
func TestTopologyChangesetCreationSecretStableAcrossDraftUpdates(t *testing.T) {
	ctx := context.Background()
	s, d := topologyDraftFixture(t)
	privateSettings := func() string {
		t.Helper()
		var raw string
		if e := s.db.QueryRow(`SELECT operations_json FROM topology_changesets WHERE id=?`, d.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var ops []TopologyOperation
		json.Unmarshal([]byte(raw), &ops)
		return ops[0].Inbound.Settings.ServerKey
	}
	before := privateSettings()
	d.Name = "Rename saved draft"
	if e := s.SaveTopologyChangeset(ctx, d); e != nil {
		t.Fatal(e)
	}
	if got := privateSettings(); got != before {
		t.Fatal("resaving regenerated secret")
	}
	d.Operations[0].Inbound.Settings = &spec.Inbound{ServerKey: "replacement"}
	if e := s.SaveTopologyChangeset(ctx, d); e == nil {
		t.Fatal("saved creation secret replaced")
	}
}
func TestTopologyChangesetResetRemovesDraftAndIncarnations(t *testing.T) {
	ctx := context.Background()
	s, d := topologyDraftFixture(t)
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: domain.RoleAdmin, Status: "active"}
	if err := s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"topology_changesets", "topology_inbound_identities", "topology_entry_identities", "topology_line_identities", "topology_ingress_identities"} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("reset retained", table, n, err)
		}
	}
	if _, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version); !errors.Is(err, ErrNotFound) {
		t.Fatal("old draft survived reset", err)
	}
}

func TestTopologyChangesetHonorsRolloutReservationsAndFrozenLine(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	preview, err := r.s.PreviewLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	intent := preview.Draft
	intent.PreviewRevision = preview.PreviewRevision
	plan, err := r.s.PlanLineRollout(ctx, &intent, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := r.s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lc TopologyLineCatalog
	for _, v := range catalog.Lines {
		if v.Line.ID == plan.LineID {
			lc = v
		}
	}
	tests := []TopologyOperation{
		{Key: "port", Kind: "inbound", Inbound: &TopologyInboundPatch{NodeID: r.nodes[0].ID, Tag: topologyPtr("reserved"), Port: topologyPtr(18443), Protocol: spec.VLESS}},
		{Key: "line", Kind: "line", ID: plan.LineID, Revision: lc.Revision, Line: &lc.Line},
		{Key: "entry", Kind: "entry", Entry: &TopologyEntryPatch{Name: topologyPtr("premature"), LineID: &plan.LineID}},
	}
	for _, op := range tests {
		t.Run(op.Key, func(t *testing.T) {
			d := &TopologyChangeset{Name: "Frozen", Operations: []TopologyOperation{op}}
			if err := r.s.SaveTopologyChangeset(ctx, d); err != nil {
				t.Fatal(err)
			}
			if _, err := r.s.PreviewTopologyChangeset(ctx, d.ID, d.Version); !errors.Is(err, ErrLineRolloutConflict) {
				t.Fatal("active rollout bypassed", err)
			}
		})
	}
}
func TestTopologyChangesetAtomicMemberReplacementAndDependencyPreview(t *testing.T) {
	ctx := context.Background()
	s, _, ib, line := lineFixture(t)
	if err := s.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	e := &domain.Entry{Name: "Published", LineID: &line.ID, Enabled: true}
	if err := s.CreateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replacement := cloneLine(line)
	replacement.Groups[0].Members[0].ID = "replacement"
	replacement.Groups[0].Members[0].Port = 18444
	d := &TopologyChangeset{Name: "Replace published member", Operations: []TopologyOperation{
		{Key: "line", Kind: "line", ID: line.ID, Revision: catalog.Lines[0].Revision, Line: replacement},
		{Key: "entry", Kind: "entry", ID: e.ID, Revision: catalog.Entries[0].Revision, Entry: &TopologyEntryPatch{LineMemberID: topologyPtr("replacement")}},
	}}
	if err = s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); err != nil {
		t.Fatal(err)
	}
	got, _ := s.EntryByID(ctx, e.ID)
	if got.DisplayPort != 18444 || got.LineMemberID != "replacement" {
		t.Fatal("final published member not applied", got)
	}
	catalog, err = s.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d = &TopologyChangeset{Name: "Landing change", Operations: []TopologyOperation{{Key: "landing", Kind: "inbound", ID: ib.ID, Revision: catalog.Inbounds[0].Revision, Inbound: &TopologyInboundPatch{Port: topologyPtr(444)}}}}
	if err = s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	p, err = s.PreviewTopologyChangeset(ctx, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range p.Changes {
		if c.Kind == "line" && c.ID == line.ID && len(c.Forwards) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("implicit compiled change absent from preview")
	}
	if _, err = s.CommitTopologyChangeset(ctx, d.ID, d.Version, p.Revision); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LineByID(ctx, line.ID)
	if l.Version != line.Version+2 {
		t.Fatal("dependent line not committed", l.Version)
	}
}

func TestTopologyChangesetSavedTargetBindingsRejectABA(t *testing.T) {
	for _, kind := range []string{"inbound", "line", "node", "ingress"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, l := lineFixture(t)
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			ingress := &Ingress{NodeID: ns[0].ID, Name: "Provider", EntryHost: "entry.example.com", PortFrom: 20000, PortTo: 30000}
			if err := s.CreateIngress(ctx, ingress); err != nil {
				t.Fatal(err)
			}
			op := TopologyOperation{Key: "new-entry", Kind: "entry", Entry: &TopologyEntryPatch{Name: topologyPtr("New"), InboundID: &ib.ID, DisplayHost: topologyPtr("192.0.2.1")}}
			table, id := "inbounds", ib.ID
			switch kind {
			case "line":
				op.Entry.InboundID = nil
				op.Entry.LineID = &l.ID
				table, id = "forward_lines", l.ID
			case "node":
				freshNode := &domain.Node{Name: "Standalone", PublicAddr: "192.0.2.99"}
				if e := s.CreateNode(ctx, freshNode, "fresh-pair", time.Hour); e != nil {
					t.Fatal(e)
				}
				ns[0] = freshNode
				op = TopologyOperation{Key: "new-inbound", Kind: "inbound", Inbound: &TopologyInboundPatch{NodeID: ns[0].ID, Tag: topologyPtr("new"), Port: topologyPtr(25000), Protocol: spec.VLESS}}
				table, id = "nodes", ns[0].ID
			case "ingress":
				op = TopologyOperation{Key: "new-inbound", Kind: "inbound", Inbound: &TopologyInboundPatch{NodeID: ns[0].ID, IngressID: &ingress.ID, Tag: topologyPtr("new"), Port: topologyPtr(25000), Protocol: spec.VLESS}}
				table, id = "ingresses", ingress.ID
			}
			d := &TopologyChangeset{Name: "Bound before first preview", Operations: []TopologyOperation{op}}
			if err := s.SaveTopologyChangeset(ctx, d); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("CREATE TEMP TABLE old_target AS SELECT * FROM "+table+" WHERE id=?", id); err != nil {
				t.Fatal(err)
			}
			// Remove forward-line dependencies before recreating an inbound. This test
			// intentionally exercises SQL-level ID reuse rather than convenient CRUD IDs.
			if kind == "inbound" {
				if _, err := s.db.Exec(`DELETE FROM forward_lines`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.db.Exec("DELETE FROM "+table+" WHERE id=?", id); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("INSERT INTO " + table + " SELECT * FROM old_target"); err != nil {
				t.Fatal(err)
			}

			// A normal rename save must retain the original dependency identities.
			d.Name = "Renamed old draft"
			if err := s.SaveTopologyChangeset(ctx, d); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version); !errors.Is(err, ErrTopologyConflict) {
				t.Fatal("saved target ID silently rebound", err)
			}
			fresh := &TopologyChangeset{Name: "Explicit fresh operation", Operations: topologyCopy(d.Operations)}
			fresh.Operations[0].Key = "new-operation"
			if fresh.Operations[0].Inbound != nil {
				fresh.Operations[0].Inbound.SettingsConfigured = false
			}
			if err := s.SaveTopologyChangeset(ctx, fresh); err != nil {
				t.Fatal(err)
			}
			// Removed source-line members do not affect a direct new inbound/Entry. A
			// recreated line itself still has its persisted groups and can be previewed.
			if _, err := s.PreviewTopologyChangeset(ctx, fresh.ID, fresh.Version); err != nil {
				t.Fatal("explicit new operation could not bind current target", err)
			}
		})
	}
}

func TestTopologyChangesetInboundCannotTakeManualForwardPort(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	fs := []NodeForward{{Forward: spec.Forward{Tag: "manual", Protocol: "tcp", Port: 25000, Target: "example.com:443"}}}
	if err := s.SetNodeForwards(ctx, ns[0].ID, fs); err != nil {
		t.Fatal(err)
	}
	d := &TopologyChangeset{Name: "Listener collision", Operations: []TopologyOperation{{Key: "inbound", Kind: "inbound", Inbound: &TopologyInboundPatch{NodeID: ns[0].ID, Tag: topologyPtr("collision"), Port: topologyPtr(25000), Protocol: spec.VLESS}}}}
	if err := s.SaveTopologyChangeset(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatal("manual listener ignored", err)
	}
}

func TestTopologyChangesetCannotIndirectlyEditManagedLanding(t *testing.T) {
	ctx := context.Background()
	s, _, ib, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	ib.Settings.WireGuardSlot = 1
	raw, _ := json.Marshal(ib.Settings)
	if _, err := s.db.Exec(`UPDATE inbounds SET settings_json=? WHERE id=?`, string(raw), ib.ID); err != nil {
		t.Fatal(err)
	}
	for _, op := range []TopologyOperation{
		{Key: "new-line", Kind: "line", Line: cloneLine(l)},
		{Key: "entry-via-line", Kind: "entry", Entry: &TopologyEntryPatch{Name: topologyPtr("Managed"), LineID: &l.ID}},
	} {
		d := &TopologyChangeset{Name: "Excluded connection", Operations: []TopologyOperation{op}}
		if err := s.SaveTopologyChangeset(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PreviewTopologyChangeset(ctx, d.ID, d.Version); !errors.Is(err, ErrTopologyUnsupported) {
			t.Fatal("managed landing accepted indirectly", err)
		}
	}
}
