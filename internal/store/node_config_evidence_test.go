package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
)

const configTestToken = "config-test-identity"
const configSessionA = "11111111111111111111111111111111"
const configSessionB = "22222222222222222222222222222222"

func configEvidenceRig(t *testing.T) (*Store, int64, time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.db")
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "Evidence test"}
	if err = s.CreateNode(context.Background(), n, "EVIDENCE", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemPairCode(context.Background(), "EVIDENCE", configTestToken, "example", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	return s, n.ID, time.Now().UTC().Truncate(time.Millisecond), path
}
func configDelivery(t *testing.T, s *Store, id int64, session, cfg, state string, at time.Time) *agentproto.ConfigDelivery {
	t.Helper()
	v, err := s.PrepareNodeConfigDelivery(context.Background(), id, configTestToken, session, cfg, state, at)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func configHistory(t *testing.T, s *Store, id int64, at time.Time) *NodeConfigHistory {
	t.Helper()
	v, err := s.NodeConfigHistory(context.Background(), id, at)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func configReceipt(d *agentproto.ConfigDelivery, cfg, state string, seq uint64, at time.Time) *agentproto.ConfigReceipt {
	return &agentproto.ConfigReceipt{DeliveryID: d.ID, Session: d.Session, ConfigRevision: cfg, StateRevision: state, Sequence: seq, ObservedAt: at, Applied: true}
}
func recordConfig(t *testing.T, s *Store, id int64, r *agentproto.ConfigReceipt, at time.Time) {
	t.Helper()
	_, err := s.RecordNodeReport(context.Background(), id, configTestToken, agentproto.Report{Revision: "watcher-may-be-ahead", AppliedRevision: r.StateRevision, ConfigReceipt: r}, false, at)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNodeConfigEvidenceDeliveryReceiptAndFreshness(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	if v := configHistory(t, s, id, at); v.Current != nil || len(v.History) != 0 {
		t.Fatal("migration invented configuration confirmation", v)
	}
	d := configDelivery(t, s, id, configSessionA, "cfg-one", "state-one", at)
	if same := configDelivery(t, s, id, configSessionA, "cfg-one", "state-one", at.Add(time.Minute)); !reflect.DeepEqual(d, same) {
		t.Fatal("304 retry changed delivery", d, same)
	}
	v := configHistory(t, s, id, at)
	if v.Current == nil || v.Current.Fresh || v.Current.Applied || v.Current.ConfirmedAt != nil || !v.Current.IssuedAt.Equal(at) {
		t.Fatal(v)
	}
	r := configReceipt(d, "cfg-one", "state-one", 1, at.Add(time.Second))
	recordConfig(t, s, id, r, r.ObservedAt)
	v = configHistory(t, s, id, r.ObservedAt)
	if !v.Current.Applied || !v.Current.Fresh || v.Current.ConfirmedAt == nil || v.Current.ReceiptSequence != 1 {
		t.Fatal(v)
	}
	first := *v.Current.ConfirmedAt
	initial := *v.Current
	// Independent config evidence is accepted even when billing reports a retry.
	if runtime, err := s.NodeRuntime(context.Background(), id); err != nil || runtime != nil {
		t.Fatal("config evidence rewrote unrelated runtime", runtime, err)
	}
	r.Applied = false
	r.ObservedAt = at.Add(2 * time.Second)
	recordConfig(t, s, id, r, r.ObservedAt)
	if got := configHistory(t, s, id, r.ObservedAt).Current; !reflect.DeepEqual(initial, *got) {
		t.Fatal("duplicate sequence changed confirmation", got)
	}
	r.Sequence = 2
	recordConfig(t, s, id, r, r.ObservedAt)
	v = configHistory(t, s, id, r.ObservedAt)
	if v.Current.Applied || !v.Current.Fresh || !v.Current.ConfirmedAt.Equal(first) {
		t.Fatal("negative receipt erased historical success or stayed green", v)
	}
	r.Sequence = 3
	r.Applied = true
	r.ObservedAt = at.Add(3 * time.Second)
	recordConfig(t, s, id, r, r.ObservedAt)
	if got := configHistory(t, s, id, r.ObservedAt).Current; !got.Applied || !got.ConfirmedAt.Equal(first) {
		t.Fatal("fresh confirmation replaced first success", got)
	}
	stale := configHistory(t, s, id, at.Add(4*time.Minute))
	if stale.Current.Fresh || !stale.Current.Applied || stale.Current.ConfirmedAt == nil {
		t.Fatal("historical success conflated with current freshness", stale)
	}
}
func TestNodeConfigEvidenceFencesChangedAndRevertedState(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	a := configDelivery(t, s, id, configSessionA, "same-structure", "state-a", at)
	recordConfig(t, s, id, configReceipt(a, "same-structure", "state-a", 1, at), at)
	b := configDelivery(t, s, id, configSessionA, "same-structure", "state-b", at.Add(time.Second))
	if a.ID == b.ID {
		t.Fatal("user/job/full-state change retained delivery")
	}
	recordConfig(t, s, id, configReceipt(a, "same-structure", "state-a", 100, at.Add(2*time.Second)), at.Add(2*time.Second))
	if got := configHistory(t, s, id, at.Add(2*time.Second)).Current; got.Applied || got.LastReceiptAt != nil {
		t.Fatal("old delivery refreshed current", got)
	}
	recordConfig(t, s, id, configReceipt(b, "same-structure", "state-b", 2, at.Add(3*time.Second)), at.Add(3*time.Second))
	if got := configHistory(t, s, id, at.Add(3*time.Second)).Current; !got.Applied {
		t.Fatal("ignored old receipt consumed process sequence", got)
	}
	back := configDelivery(t, s, id, configSessionA, "same-structure", "state-a", at.Add(4*time.Second))
	if back.ID == a.ID || back.ID == b.ID {
		t.Fatal("returning to an old state reused old delivery")
	}
	recordConfig(t, s, id, configReceipt(a, "same-structure", "state-a", 101, at.Add(5*time.Second)), at.Add(5*time.Second))
	if got := configHistory(t, s, id, at.Add(5*time.Second)).Current; got.Applied {
		t.Fatal("old A receipt confirmed reverted A")
	}
	next, err := s.PrepareNodeConfigDelivery(context.Background(), id, configTestToken, configSessionA, "cfg2-projection", "state-a", at.Add(6*time.Second))
	if err != nil || next == nil || next.ID == back.ID {
		t.Fatal("projection-version change could not issue new delivery", next, err)
	}
	recordConfig(t, s, id, configReceipt(back, "same-structure", "state-a", 102, at.Add(7*time.Second)), at.Add(7*time.Second))
	if got := configHistory(t, s, id, at.Add(7*time.Second)).Current; got.ID != next.ID || got.Applied {
		t.Fatal("old projection confirmed new delivery", got)
	}
}
func TestNodeConfigEvidenceRetiredSessionsAndTokenRotation(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	ctx := context.Background()
	a := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(a, "cfg", "state", 7, at), at)
	b := configDelivery(t, s, id, configSessionB, "cfg", "state", at.Add(time.Second))
	if got := configDelivery(t, s, id, configSessionA, "cfg", "state-new", at.Add(2*time.Second)); got != nil {
		t.Fatal("retired process reclaimed delivery", got)
	}
	recordConfig(t, s, id, configReceipt(a, "cfg", "state", 100, at.Add(2*time.Second)), at.Add(2*time.Second))
	if got := configHistory(t, s, id, at.Add(2*time.Second)).Current; got.ID != b.ID || got.Applied {
		t.Fatal(got)
	}
	recordConfig(t, s, id, configReceipt(b, "cfg", "state", 1, at.Add(3*time.Second)), at.Add(3*time.Second))
	if got := configHistory(t, s, id, at.Add(3*time.Second)).Current; !got.Fresh || !got.Applied {
		t.Fatal(got)
	}
	if err := s.ResetPairCode(ctx, id, "NEWPAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	unpaired := configHistory(t, s, id, at.Add(4*time.Second))
	if unpaired.Current != nil || len(unpaired.History) != 2 {
		t.Fatal(unpaired)
	}
	for _, v := range unpaired.History {
		if v.Current || v.Fresh {
			t.Fatal("revoked identity still current", v)
		}
	}
	if _, err := s.PrepareNodeConfigDelivery(ctx, id, configTestToken, configSessionB, "cfg", "state", at); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked token delivered state", err)
	}
	if _, err := s.RedeemPairCode(ctx, "NEWPAIR", "new-config-identity", "example", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordNodeReport(ctx, id, configTestToken, agentproto.Report{ConfigReceipt: configReceipt(b, "cfg", "state", 101, at)}, true, at); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token recorded receipt", err)
	}
	// A new token generation can reuse a process session string without inheriting its evidence.
	next, err := s.PrepareNodeConfigDelivery(ctx, id, "new-config-identity", configSessionA, "cfg", "state", at.Add(5*time.Second))
	if err != nil || next == nil {
		t.Fatal(next, err)
	}
	stale := configReceipt(a, "cfg", "state", 200, at.Add(6*time.Second))
	if _, err := s.RecordNodeReport(ctx, id, "new-config-identity", agentproto.Report{AppliedRevision: "state", ConfigReceipt: stale}, true, stale.ObservedAt); err != nil {
		t.Fatal(err)
	}
	got := configHistory(t, s, id, stale.ObservedAt)
	if got.Current.ID != next.ID || got.Current.Applied {
		t.Fatal("previous token generation receipt was promoted", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{configTestToken, "new-config-identity", "token_identity", "token_hash"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private authentication identity leaked", secret)
		}
	}
}
func TestNodeConfigEvidenceRejectsUntrustedReceiptTimesAndIdentity(t *testing.T) {
	edits := map[string]func(*agentproto.ConfigReceipt){
		"zero sequence":             func(r *agentproto.ConfigReceipt) { r.Sequence = 0 },
		"overflow sequence":         func(r *agentproto.ConfigReceipt) { r.Sequence = math.MaxUint64 },
		"old observation":           func(r *agentproto.ConfigReceipt) { r.ObservedAt = r.ObservedAt.Add(-4 * time.Minute) },
		"future observation":        func(r *agentproto.ConfigReceipt) { r.ObservedAt = r.ObservedAt.Add(31 * time.Second) },
		"before delivery":           func(r *agentproto.ConfigReceipt) { r.ObservedAt = r.ObservedAt.Add(-31 * time.Second) },
		"zero observation":          func(r *agentproto.ConfigReceipt) { r.ObservedAt = time.Time{} },
		"wrong session":             func(r *agentproto.ConfigReceipt) { r.Session = configSessionB },
		"wrong delivery":            func(r *agentproto.ConfigReceipt) { r.DeliveryID = "other" },
		"wrong structural revision": func(r *agentproto.ConfigReceipt) { r.ConfigRevision = "other" },
		"wrong state revision":      func(r *agentproto.ConfigReceipt) { r.StateRevision = "other" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			s, id, at, _ := configEvidenceRig(t)
			d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
			r := configReceipt(d, "cfg", "state", 1, at)
			edit(r)
			recordConfig(t, s, id, r, at)
			if got := configHistory(t, s, id, at).Current; got.LastReceiptAt != nil || got.Applied || got.ConfirmedAt != nil {
				t.Fatal("invalid evidence recorded", got)
			}
		})
	}
}
func TestNodeConfigEvidenceAppliedRevisionAndMonotonicEvidence(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	r := configReceipt(d, "cfg", "state", 1, at.Add(time.Second))
	_, err := s.RecordNodeReport(context.Background(), id, configTestToken, agentproto.Report{Revision: "state", AppliedRevision: "old-state", ConfigReceipt: r}, true, r.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := configHistory(t, s, id, r.ObservedAt).Current; got.Applied || !got.Fresh || got.ConfirmedAt != nil {
		t.Fatal("boolean replaced full applied-revision evidence", got)
	}
	r.Sequence = 2
	r.ObservedAt = at.Add(3 * time.Second)
	recordConfig(t, s, id, r, r.ObservedAt)
	original := *configHistory(t, s, id, r.ObservedAt).Current
	r.Sequence = 3
	r.ObservedAt = at.Add(2 * time.Second)
	r.Applied = false
	recordConfig(t, s, id, r, at.Add(4*time.Second))
	if got := configHistory(t, s, id, at.Add(4*time.Second)).Current; !reflect.DeepEqual(original, *got) {
		t.Fatal("new sequence with older observation rewrote evidence", got)
	}
	r.ObservedAt = at.Add(4 * time.Second)
	recordConfig(t, s, id, r, at.Add(2*time.Second))
	if got := configHistory(t, s, id, at.Add(4*time.Second)).Current; !reflect.DeepEqual(original, *got) {
		t.Fatal("server clock rollback refreshed evidence", got)
	}
}
func TestNodeConfigEvidenceHistoryBoundAndSessionTombstones(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	configDelivery(t, s, id, configSessionA, "cfg", "first", at)
	var last *agentproto.ConfigDelivery
	for i := 0; i < 125; i++ {
		last = configDelivery(t, s, id, configSessionB, "cfg", fmt.Sprintf("state-%d", i), at.Add(time.Duration(i+1)*time.Millisecond))
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM node_config_deliveries WHERE node_id=?`, id).Scan(&count); err != nil || count != 100 {
		t.Fatal(count, err)
	}
	got := configHistory(t, s, id, at.Add(time.Second))
	if len(got.History) != 20 || got.Current.ID != last.ID || got.History[0].ID != last.ID {
		t.Fatal(got)
	}
	if old := configDelivery(t, s, id, configSessionA, "cfg", "first", at.Add(time.Second)); old != nil {
		t.Fatal("history pruning revived a retired process", old)
	}
}
func TestNodeConfigEvidenceTransactionsRollback(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	ctx := context.Background()
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_delivery BEFORE INSERT ON node_config_deliveries BEGIN SELECT RAISE(ABORT,'delivery failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareNodeConfigDelivery(ctx, id, configTestToken, configSessionB, "cfg", "state-2", at); err == nil {
		t.Fatal("delivery insert failure ignored")
	}
	var retired any
	if err := s.db.QueryRow(`SELECT retired_at FROM node_config_sessions WHERE node_id=? AND session=?`, id, configSessionA).Scan(&retired); err != nil || retired != nil {
		t.Fatal("failed delivery retired live session", retired, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_delivery`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_receipt BEFORE UPDATE ON node_config_deliveries BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	rep := agentproto.Report{Version: "would-be-written", AppliedRevision: "state", ConfigReceipt: configReceipt(d, "cfg", "state", 1, at)}
	if _, err := s.RecordNodeReport(ctx, id, configTestToken, rep, true, at); err == nil {
		t.Fatal("receipt failure ignored")
	}
	if runtime, err := s.NodeRuntime(ctx, id); err != nil || runtime != nil {
		t.Fatal("report partially committed", runtime, err)
	}
	var seq int
	if err := s.db.QueryRow(`SELECT receipt_sequence FROM node_config_sessions WHERE node_id=? AND session=?`, id, configSessionA).Scan(&seq); err != nil || seq != 0 {
		t.Fatal("failed receipt consumed sequence", seq, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_receipt`); err != nil {
		t.Fatal(err)
	}
	recordConfig(t, s, id, rep.ConfigReceipt, at)
	if got := configHistory(t, s, id, at).Current; !got.Applied {
		t.Fatal("retry after transaction rollback was discarded", got)
	}
}
func TestNodeConfigEvidenceReopenAndMigrationPreserveLegacy(t *testing.T) {
	s, id, at, path := configEvidenceRig(t)
	a := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(a, "cfg", "state", 1, at), at)
	b := configDelivery(t, s, id, configSessionB, "cfg", "state", at.Add(time.Second))
	recordConfig(t, s, id, configReceipt(b, "cfg", "state", 1, at.Add(time.Second)), at.Add(time.Second))
	before := configHistory(t, s, id, at.Add(time.Second))
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s = New(reopened)
	if got := configHistory(t, s, id, at.Add(time.Second)); !reflect.DeepEqual(before, got) {
		t.Fatal("restart changed durable history", before, got)
	}
	if got := configDelivery(t, s, id, configSessionA, "cfg", "state", at.Add(2*time.Second)); got != nil {
		t.Fatal("restart forgot retired session")
	}
	r := configReceipt(b, "cfg", "state", 1, at.Add(2*time.Second))
	r.Applied = false
	recordConfig(t, s, id, r, r.ObservedAt)
	if got := configHistory(t, s, id, r.ObservedAt).Current; !got.Applied || got.ReceiptSequence != 1 {
		t.Fatal("restart forgot accepted receipt sequence", got)
	}
	// Upgrade a real schema-99 database without treating legacy applied_revision
	// (which is fetched revision) as modern configuration confirmation.
	old, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(context.Background(), old, ".", 99); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`INSERT INTO nodes(id,name,token_hash,applied_revision,created_at,updated_at) VALUES(1,'Legacy','legacy-identity','fetched-not-applied',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(context.Background(), old, "sqlite"); err != nil {
		t.Fatal(err)
	}
	migrated := New(old)
	history := configHistory(t, migrated, 1, at)
	if history.Current != nil || len(history.History) != 0 {
		t.Fatal("migration fabricated confirmation", history)
	}
	n, err := migrated.NodeByTokenHash(context.Background(), "legacy-identity")
	if err != nil || n.ID != 1 || n.AppliedRevision != "fetched-not-applied" {
		t.Fatal("migration changed legacy identity/revision", n, err)
	}
}
func TestNodeConfigEvidenceDeleteAndIDReuse(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(d, "cfg", "state", 1, at), at)
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_config_sessions", "node_config_heads", "node_config_deliveries"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id,name,token_hash,created_at,updated_at) VALUES(?,'Replacement',?,1,1)`, id, configTestToken); err != nil {
		t.Fatal(err)
	}
	if got := configHistory(t, s, id, at); got.Current != nil || len(got.History) != 0 {
		t.Fatal("reused ID inherited old evidence", got)
	}
	recordConfig(t, s, id, configReceipt(d, "cfg", "state", 2, at), at)
	if got := configHistory(t, s, id, at); got.Current != nil || len(got.History) != 0 {
		t.Fatal("retired incarnation receipt recreated evidence", got)
	}
}

func TestNodeConfigEvidenceSiteResetClearsFencesAndHistory(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	ctx := context.Background()
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	recordConfig(t, s, id, configReceipt(d, "cfg", "state", 1, at), at)
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "fixture", Role: domain.RoleAdmin, Status: "active"}
	if err := s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_config_sessions", "node_config_heads", "node_config_deliveries"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}
