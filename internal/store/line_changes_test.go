package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func lineChangeLatest(t *testing.T, s *Store, id int64, at time.Time) *LineChange {
	t.Helper()
	change, err := s.LatestLineChangeID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.LineChangeByID(context.Background(), change, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func lineChangeRead(t *testing.T, s *Store, id string, at time.Time) *LineChange {
	t.Helper()
	c, err := s.LineChangeByID(context.Background(), id, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func lineChangeTarget(t *testing.T, c *LineChange, id int64) LineChangeTarget {
	t.Helper()
	for _, v := range c.Targets {
		if v.NodeID == id {
			return v
		}
	}
	t.Fatalf("missing target %d: %+v", id, c)
	return LineChangeTarget{}
}
func lineChangePair(t *testing.T, s *Store, id int64) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE nodes SET token_hash=? WHERE id=?`, configTestToken, id); err != nil {
		t.Fatal(err)
	}
}
func lineChangeWire(t *testing.T, s *Store, id int64) []spec.Forward {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	fs, err := nodeForwardsTx(context.Background(), tx, id)
	if err != nil {
		t.Fatal(err)
	}
	out := []spec.Forward{}
	for _, f := range fs {
		f.IngressID = ""
		out = append(out, f.Forward)
	}
	return out
}
func lineChangeDelivery(t *testing.T, s *Store, id int64, session, cfg, state string, at time.Time, seen ...string) *agentproto.ConfigDelivery {
	t.Helper()
	d, err := s.PrepareNodeConfigDeliveryForLines(context.Background(), id, configTestToken, session, cfg, state, lineChangeWire(t, s, id), at, seen...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestLineChangesMetadataAndPartialTargets(t *testing.T) {
	ctx := WithLineChangeActor(context.Background(), "admin@example.com")
	s, ns, _, l := lineFixture(t)
	at := time.Now()
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	first := lineChangeLatest(t, s, l.ID, at)
	if first.Actor != "admin@example.com" || first.Kind != "save" || len(first.Targets) != 3 {
		t.Fatalf("wrong creation: %+v", first)
	}
	for _, v := range first.Targets {
		if v.NodeID == ns[3].ID {
			t.Fatal("landing dependency presented as modified target")
		}
	}
	l.Name = "metadata only"
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	meta := lineChangeLatest(t, s, l.ID, at)
	if meta.Status != "no_runtime_change" || len(meta.Targets) != 0 {
		t.Fatal(meta)
	}
	if got := lineChangeRead(t, s, first.ID, at); got.Status != "pending" || got.SupersededAt != nil {
		t.Fatal("metadata save erased pending runtime targets", got)
	}
	l.Groups[1].Members[1].Port++
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	partial := lineChangeLatest(t, s, l.ID, at)
	if len(partial.Targets) != 2 {
		t.Fatal("expected entry and changed member only", partial)
	}
	old := lineChangeRead(t, s, first.ID, at)
	if lineChangeTarget(t, old, ns[1].ID).Status != "pending" || lineChangeTarget(t, old, ns[0].ID).Status != "superseded" || old.SupersededAt != nil {
		t.Fatal("partial edit superseded unchanged node", old)
	}
	// Metadata records cannot displace old per-node baselines from bounded history.
	for i := 0; i < 105; i++ {
		l.Name = fmt.Sprintf("metadata %d", i)
		if err := s.SaveLine(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if lineChangeTarget(t, lineChangeRead(t, s, first.ID, at), ns[1].ID).Status != "pending" {
		t.Fatal("retention discarded active old baseline")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM line_changes`).Scan(&count); err != nil || count != 102 {
		t.Fatal("unexpected retained history", count, err)
	}
	page, err := s.ListLineChanges(ctx, l.ID, 0, 2)
	if err != nil || len(page.Items) != 2 || page.NextBefore == 0 {
		t.Fatal(page, err)
	}
	next, err := s.ListLineChanges(ctx, l.ID, page.NextBefore, 2)
	if err != nil || len(next.Items) != 2 || next.Items[0].Cursor >= page.Items[1].Cursor {
		t.Fatal("pagination repeated cursor", next, err)
	}
}
func TestLineChangesTransactionRollback(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	old, err := s.LineByID(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.NodeForwards(ctx, ns[0].ID)
	first := lineChangeLatest(t, s, l.ID, time.Now())
	if _, err := s.db.Exec(`CREATE TRIGGER fail_line_change BEFORE INSERT ON line_changes BEGIN SELECT RAISE(ABORT,'injected history failure'); END`); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].Port++
	if err := s.SaveLine(ctx, l); err == nil {
		t.Fatal("history failure did not abort SaveLine")
	}
	live, err := s.LineByID(ctx, l.ID)
	if err != nil || !reflect.DeepEqual(live, old) {
		t.Fatal("failed record changed line", live, old, err)
	}
	after, _ := s.NodeForwards(ctx, ns[0].ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed record changed forwards")
	}
	if got := lineChangeLatest(t, s, l.ID, time.Now()); got.ID != first.ID || got.SupersededAt != nil {
		t.Fatal("failed record changed target history", got)
	}
	if err := s.DeleteLine(ctx, l.ID, old.Version); err == nil {
		t.Fatal("history failure did not abort DeleteLine")
	}
	if _, err := s.LineByID(ctx, l.ID); err != nil {
		t.Fatal("failed deletion removed line", err)
	}
}
func TestLineChangesABARequiresFreshDeliveryAndReceipt(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[0].ID
	lineChangePair(t, s, id)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	old := lineChangeLatest(t, s, l.ID, at)
	d := lineChangeDelivery(t, s, id, configSessionA, "cfg-a", "state-a", at)
	recordConfig(t, s, id, configReceipt(d, "cfg-a", "state-a", 1, at), at)
	if lineChangeTarget(t, lineChangeRead(t, s, old.ID, at), id).Status != "applied" {
		t.Fatal("receipt did not confirm line")
	}
	l.Groups[0].Members[0].Port++
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].Port--
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	latest := lineChangeLatest(t, s, l.ID, at)
	newer := lineChangeDelivery(t, s, id, configSessionA, "cfg-a", "state-a", at.Add(time.Second), d.ID)
	if newer.ID == d.ID {
		t.Fatal("ABA reused previously confirmed delivery")
	}
	recordConfig(t, s, id, configReceipt(d, "cfg-a", "state-a", 100, at.Add(time.Second)), at.Add(time.Second))
	got := lineChangeTarget(t, lineChangeRead(t, s, latest.ID, at.Add(time.Second)), id)
	if got.Status != "delivered" || got.ConfirmedAt != nil {
		t.Fatal("old A confirmed new A", got)
	}
	recordConfig(t, s, id, configReceipt(newer, "cfg-a", "state-a", 2, at.Add(2*time.Second)), at.Add(2*time.Second))
	got = lineChangeTarget(t, lineChangeRead(t, s, latest.ID, at.Add(2*time.Second)), id)
	if got.Status != "applied" || got.FreshForMS <= 0 {
		t.Fatal("new receipt not copied", got)
	}
	first := *got.ConfirmedAt
	next := lineChangeDelivery(t, s, id, configSessionA, "cfg-a", "state-users-changed", at.Add(3*time.Second))
	got = lineChangeTarget(t, lineChangeRead(t, s, latest.ID, at.Add(3*time.Second)), id)
	if got.Status != "delivered" || !got.ConfirmedAt.Equal(first) {
		t.Fatal("dynamic state inherited current green or erased history", got)
	}
	r := configReceipt(next, "cfg-a", "state-users-changed", 3, at.Add(4*time.Second))
	r.Applied = false
	recordConfig(t, s, id, r, r.ObservedAt)
	got = lineChangeTarget(t, lineChangeRead(t, s, latest.ID, r.ObservedAt), id)
	if got.Status != "not_applied" || !got.ConfirmedAt.Equal(first) {
		t.Fatal("negative receipt lost historical success", got)
	}
	r.Applied = true
	r.ObservedAt = r.ObservedAt.Add(time.Second)
	recordConfig(t, s, id, r, r.ObservedAt)
	if v := lineChangeTarget(t, lineChangeRead(t, s, latest.ID, r.ObservedAt), id); v.Status != "not_applied" || !v.LastReceiptAt.Equal(*got.LastReceiptAt) {
		t.Fatal("duplicate refreshed line", v)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), configTestToken) || strings.Contains(string(raw), "token_identity") {
		t.Fatal("identity leaked in DTO")
	}
}
func TestLineChangesPendingSessionCannotReplaceActiveEvidence(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[0].ID
	lineChangePair(t, s, id)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	c := lineChangeLatest(t, s, l.ID, at)
	a := lineChangeDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(a, "cfg", "state", 1, at), at)
	b := lineChangeDelivery(t, s, id, configSessionB, "cfg", "state", at.Add(time.Second))
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(time.Second)), id); got.Status != "applied" || got.DeliveryID != a.ID {
		t.Fatal("delayed first GET displaced active line confirmation", got)
	}
	same := lineChangeDelivery(t, s, id, configSessionB, "cfg", "state", at.Add(2*time.Second), b.ID)
	if same.ID != b.ID {
		t.Fatal("candidate changed during handshake")
	}
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(2*time.Second)), id); got.Status != "delivered" || got.DeliveryID != b.ID {
		t.Fatal("activated candidate did not bind", got)
	}
	recordConfig(t, s, id, configReceipt(b, "cfg", "state", 1, at.Add(3*time.Second)), at.Add(3*time.Second))
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(3*time.Second)), id); got.Status != "applied" {
		t.Fatal(got)
	}
}
func TestLineChangesReceiptEvidenceSurvivesDeliveryPruning(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[0].ID
	lineChangePair(t, s, id)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	c := lineChangeLatest(t, s, l.ID, at)
	d := lineChangeDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(d, "cfg", "state", 1, at), at)
	for i := 0; i < 105; i++ {
		configDelivery(t, s, id, configSessionA, "cfg", fmt.Sprintf("state-%d", i), at.Add(time.Duration(i+1)*time.Millisecond))
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM node_config_deliveries WHERE id=?`, d.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("old delivery not pruned", n, err)
	}
	got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(time.Second)), id)
	if got.Status != "stale" || got.ConfirmedAt == nil || got.LastReceiptAt == nil || got.DeliveryID != d.ID {
		t.Fatal("delivery retention removed independent line evidence", got)
	}
	again := lineChangeDelivery(t, s, id, configSessionA, "cfg", "state-104", at.Add(2*time.Second))
	recordConfig(t, s, id, configReceipt(again, "cfg", "state-104", 2, at.Add(3*time.Second)), at.Add(3*time.Second))
	got = lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(3*time.Second)), id)
	if got.Status != "applied" || !got.ConfirmedAt.Equal(at) {
		t.Fatal("new delivery failed to preserve first confirmation", got)
	}
}

func TestLineChangesRemovalScopeAndNodeIdentity(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[2].ID
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	// Initially unpaired targets may bind only their first authenticated identity.
	lineChangePair(t, s, id)
	l.Groups[1].Members = l.Groups[1].Members[:1]
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	removed := lineChangeLatest(t, s, l.ID, at)
	if v := lineChangeTarget(t, removed, id); v.Action != "remove" || len(v.After) != 0 {
		t.Fatal(v)
	}
	if err := s.DeleteLine(ctx, l.ID, l.Version); err != nil {
		t.Fatal(err)
	}
	deleted := lineChangeLatest(t, s, l.ID, at)
	if deleted.Kind != "delete" || len(deleted.Targets) != 2 {
		t.Fatal("delete lost current cleanup targets", deleted)
	}
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at), id); v.Status != "pending" {
		t.Fatal("delete superseded earlier removed-member cleanup", v)
	}
	// An extra old rule in the delivered prefix prevents a false cleanup binding.
	oldRule := spec.Forward{Tag: fmt.Sprintf("line-%d-unexpected", l.ID), Protocol: "tcp", Port: 19443, Target: "example.com:443"}
	if _, err := s.PrepareNodeConfigDeliveryForLines(ctx, id, configTestToken, configSessionA, "cfg-old", "state-old", []spec.Forward{oldRule}, at); err != nil {
		t.Fatal(err)
	}
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at), id); v.DeliveryID != "" {
		t.Fatal("prefix debris counted as cleanup", v)
	}
	d := lineChangeDelivery(t, s, id, configSessionA, "cfg-empty", "state-empty", at.Add(time.Second))
	recordConfig(t, s, id, configReceipt(d, "cfg-empty", "state-empty", 1, at.Add(time.Second)), at.Add(time.Second))
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at.Add(time.Second)), id); v.Status != "applied" {
		t.Fatal("tombstoned scope could not confirm cleanup", v)
	}
	if err := s.ResetPairCode(ctx, id, "repair", time.Hour); err != nil {
		t.Fatal(err)
	}
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at.Add(time.Second)), id); v.Status != "identity_changed" || v.FreshForMS != 0 {
		t.Fatal("cleared token retained green", v)
	}
	if _, err := s.RedeemPairCode(ctx, "repair", "next-token", "example", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareNodeConfigDeliveryForLines(ctx, id, "next-token", configSessionA, "cfg-empty", "state-empty", nil, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at.Add(2*time.Second)), id); v.Status != "identity_changed" || v.DeliveryID != d.ID {
		t.Fatal("new pairing borrowed prior target", v)
	}
	if err := s.DeleteNode(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id,name,token_hash,created_at,updated_at) VALUES(?,'Replacement','next-token',1,1)`, id); err != nil {
		t.Fatal(err)
	}
	if v := lineChangeTarget(t, lineChangeRead(t, s, removed.ID, at.Add(2*time.Second)), id); v.Status != "removed" {
		t.Fatal("reused node ID revived historical target", v)
	}
	// Reuse a numeric line ID: its new scope must own listing and cannot revive old targets.
	if _, err := s.db.Exec(`INSERT INTO forward_lines(id,name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(?, 'Replacement line',1,'tcp','relay',?,'[]',1)`, l.ID, l.TargetInboundID); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListLineChanges(ctx, l.ID, 0, 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("new scope exposed prior incarnation history", page, err)
	}
	if got := lineChangeRead(t, s, deleted.ID, at); got.ScopeID != deleted.ScopeID || got.Status != "superseded" {
		t.Fatal("scope identity changed", got)
	}
}

func TestLineChangesRecompileAndExactWireProjection(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[0].ID
	lineChangePair(t, s, id)
	at := time.Now().UTC().Truncate(time.Millisecond)
	g := &Ingress{NodeID: id, Name: "NAT", Kind: "nat", BindIP: "10.10.0.2", EntryHost: "203.0.113.30"}
	if err := s.CreateIngress(ctx, g); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].IngressID = &g.ID
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	c := lineChangeLatest(t, s, l.ID, at)
	if got := lineChangeTarget(t, c, id); got.After[0].Listen != g.BindIP {
		t.Fatal("projection omitted resolved listener", got)
	}
	// Capability filtering removes the rule: issuance continues but cannot bind its target.
	if _, err := s.PrepareNodeConfigDeliveryForLines(ctx, id, configTestToken, configSessionA, "cfg-filter", "state-filter", nil, at); err != nil {
		t.Fatal(err)
	}
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at), id); got.DeliveryID != "" {
		t.Fatal("filtered rule counted as delivered", got)
	}
	wire := lineChangeWire(t, s, id)
	wire = append(wire, spec.Forward{Tag: "manual", Port: 22000, Target: "example.com:443"})
	d, err := s.PrepareNodeConfigDeliveryForLines(ctx, id, configTestToken, configSessionA, "cfg", "state", wire, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	recordConfig(t, s, id, configReceipt(d, "cfg", "state", 1, at.Add(time.Second)), at.Add(time.Second))
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(time.Second)), id); got.Status != "applied" {
		t.Fatal("unrelated forwards prevented line confirmation", got)
	}
	n, err := s.NodeByID(ctx, ns[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	n.PublicAddr = "192.0.2.10"
	if err = s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	next := lineChangeLatest(t, s, l.ID, at.Add(2*time.Second))
	if next.Kind != "recompile" || next.Actor != "system" || len(next.Targets) != 1 || next.Targets[0].NodeID != id {
		t.Fatal("implicit recompile lacked atomic target change", next)
	}
	// Cached old State cannot bind even though it belongs to this line's prefix.
	if _, err = s.PrepareNodeConfigDeliveryForLines(ctx, id, configTestToken, configSessionA, "cfg", "state", wire, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := lineChangeTarget(t, lineChangeRead(t, s, next.ID, at.Add(2*time.Second)), id); got.DeliveryID != "" {
		t.Fatal("stale cache bound new plan", got)
	}
}

func TestLineChangesCandidateReceiptFencesAndAtomicCopy(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	id := ns[0].ID
	lineChangePair(t, s, id)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	c := lineChangeLatest(t, s, l.ID, at)
	a := lineChangeDelivery(t, s, id, configSessionA, "cfg", "state", at)
	b := lineChangeDelivery(t, s, id, configSessionB, "cfg", "state", at.Add(time.Second))
	thirdSession := "33333333333333333333333333333333"
	third := lineChangeDelivery(t, s, id, thirdSession, "cfg", "state", at.Add(time.Second))
	// A receipt, not just an echoed GET, may activate a valid candidate.
	recordConfig(t, s, id, configReceipt(b, "cfg", "state", 1, at.Add(2*time.Second)), at.Add(2*time.Second))
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(2*time.Second)), id); got.Status != "applied" || got.DeliveryID != b.ID {
		t.Fatal("candidate receipt failed to activate mapping", got)
	}
	for _, old := range []*agentproto.ConfigDelivery{a, third} {
		recordConfig(t, s, id, configReceipt(old, "cfg", "state", 100, at.Add(3*time.Second)), at.Add(3*time.Second))
	}
	got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, at.Add(3*time.Second)), id)
	if got.DeliveryID != b.ID || !got.LastReceiptAt.Equal(at.Add(2*time.Second)) {
		t.Fatal("retired or stale-parent candidate refreshed line", got)
	}
	// The target copy is part of the report transaction, including process seq.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_target_receipt BEFORE UPDATE ON line_change_targets BEGIN SELECT RAISE(ABORT,'target receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	r := configReceipt(b, "cfg", "state", 2, at.Add(4*time.Second))
	if _, err := s.RecordNodeReport(ctx, id, configTestToken, agentproto.Report{AppliedRevision: r.StateRevision, ConfigReceipt: r}, false, r.ObservedAt); err == nil {
		t.Fatal("target receipt error swallowed")
	}
	if v := configHistory(t, s, id, r.ObservedAt).Current; v.ReceiptSequence != 1 {
		t.Fatal("failed target copy consumed receipt", v)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_target_receipt`); err != nil {
		t.Fatal(err)
	}
	recordConfig(t, s, id, r, r.ObservedAt)
	if got := lineChangeTarget(t, lineChangeRead(t, s, c.ID, r.ObservedAt), id); !got.LastReceiptAt.Equal(r.ObservedAt) {
		t.Fatal("retry after rollback lost", got)
	}
}

func TestLineChangesMigrationRestartAndReset(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "line-history.db")
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, conn, ".", 100); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes(id,name,public_addr,version,created_at,updated_at) VALUES(1,'relay','192.0.2.10','v0.83.0',1,1),(2,'landing','198.51.100.20','v0.83.0',1,1)`,
		`INSERT INTO inbounds(id,node_id,tag,protocol,port,created_at,updated_at) VALUES(1,2,'landing','vless',443,1,1)`,
		`INSERT INTO forward_lines(id,name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(1,'Legacy',1,'tcp','relay',1,'[{"strategy":"failover","members":[{"id":"legacy-member","node_id":1,"port":18443}]}]',1)`,
	} {
		if _, err = conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	page, err := s.ListLineChanges(ctx, 1, 0, 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("migration invented legacy confirmation", page, err)
	}
	if latest, err := s.LatestLineChangeID(ctx, 1); err != nil || latest != "" {
		t.Fatal(latest, err)
	}
	line, err := s.LineByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	change, err := s.SaveLineWithChange(ctx, line)
	if err != nil || change == "" {
		t.Fatal(change, err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	lineChangePair(t, s, 1)
	d := lineChangeDelivery(t, s, 1, configSessionA, "cfg", "state", at)
	recordConfig(t, s, 1, configReceipt(d, "cfg", "state", 1, at), at)
	before := lineChangeRead(t, s, change, at)
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err = db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s = New(conn)
	if after := lineChangeRead(t, s, change, at); !reflect.DeepEqual(before, after) {
		t.Fatal("restart changed durable line evidence", before, after)
	}
	del, err := s.DeleteLineWithChange(ctx, 1, line.Version)
	if err != nil || del == "" || del == change {
		t.Fatal("missing exact delete change", del, err)
	}
	if latest, err := s.LatestLineChangeID(ctx, 1); err != nil || latest != del {
		t.Fatal("delete lost tombstone navigation", latest, err)
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "fixture", Role: domain.RoleAdmin, Status: "active"}
	if err = s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"line_change_scopes", "line_changes", "line_change_targets", "line_change_deliveries"} {
		var n int
		if err = conn.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}
