package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"testing"
	"time"
)

func renderedFixture(t *testing.T) *agentproto.RenderedConfigReport {
	t.Helper()
	var v agentproto.RenderedConfigReport
	if err := json.Unmarshal([]byte(`{"version":1,"status":"observed","artifacts":[]}`), &v); err != nil {
		t.Fatal(err)
	}
	return &v
}
func TestRenderedConfigAcceptedReceiptOwnsEvidence(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	ctx := context.Background()
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	receipt := configReceipt(d, "cfg", "state", 1, at)
	send := func(v *agentproto.RenderedConfigReport) {
		t.Helper()
		_, err := s.RecordNodeReport(ctx, id, configTestToken, agentproto.Report{ConfigReceipt: receipt, AppliedRevision: receipt.StateRevision, RenderedConfig: v}, false, receipt.ObservedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(now time.Time) *NodeRenderedConfigRecord {
		t.Helper()
		h, err := s.NodeRenderedConfigHistory(ctx, id, now)
		if err != nil {
			t.Fatal(err)
		}
		return h.Current
	}
	send(renderedFixture(t))
	if v := read(at); v == nil || v.MetadataStatus != "valid" || v.Evidence == nil || !v.Fresh {
		t.Fatal(v)
	}
	send(nil)
	if v := read(at); v.Evidence == nil {
		t.Fatal("duplicate receipt changed accepted evidence")
	}
	receipt.Sequence++
	receipt.ObservedAt = at.Add(time.Second)
	send(nil)
	if v := read(receipt.ObservedAt); v.Evidence != nil || v.MetadataStatus != "unavailable" {
		t.Fatal("missing optional metadata borrowed old artifact", v)
	}
	receipt.Sequence++
	receipt.ObservedAt = at.Add(2 * time.Second)
	send(&agentproto.RenderedConfigReport{Version: -1})
	if v := read(receipt.ObservedAt); v.Evidence != nil || v.MetadataStatus != "invalid" {
		t.Fatal(v)
	}
	receipt.Sequence++
	receipt.ObservedAt = at.Add(3 * time.Second)
	send(renderedFixture(t))
	if v := read(at.Add(4 * time.Minute)); v.Fresh || v.Evidence == nil {
		t.Fatal("old evidence must remain historical without current freshness", v)
	}
	if v := read(at.Add(-time.Minute)); v.Fresh {
		t.Fatal("clock reversal made evidence live")
	}
	receipt.Sequence++
	receipt.ObservedAt = at.Add(4 * time.Second)
	receipt.Applied = false
	send(renderedFixture(t))
	if v := read(receipt.ObservedAt); v.Applied {
		t.Fatal("negative receipt borrowed success")
	}
	if _, err := s.db.Exec(`UPDATE nodes SET token_hash='replacement' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if v := read(receipt.ObservedAt); v != nil {
		t.Fatal("re-pair inherited current evidence")
	}
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM node_rendered_configs`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestRenderedConfigCurrentSurvivesHistoryWindowAndRollback(t *testing.T) {
	s, id, at, _ := configEvidenceRig(t)
	ctx := context.Background()
	d := configDelivery(t, s, id, configSessionA, "cfg", "state", at)
	r := configReceipt(d, "cfg", "state", 1, at)
	report := agentproto.Report{ConfigReceipt: r, AppliedRevision: "state", RenderedConfig: renderedFixture(t)}
	if _, err := s.RecordNodeReport(ctx, id, configTestToken, report, false, at); err != nil {
		t.Fatal(err)
	}
	// New candidate sessions do not evict the actual active head from the view.
	for i := 1; i <= 25; i++ {
		configDelivery(t, s, id, fmt.Sprintf("%032x", i), "cfg", "state", at.Add(time.Second))
	}
	h, err := s.NodeRenderedConfigHistory(ctx, id, at.Add(time.Second))
	if err != nil || h.Current == nil || h.Current.ID != d.ID || h.Current.Evidence == nil || len(h.History) > 21 {
		t.Fatal(h, err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_rendered BEFORE UPDATE ON node_rendered_configs BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	r.Sequence = 2
	r.ObservedAt = at.Add(2 * time.Second)
	if _, err = s.RecordNodeReport(ctx, id, configTestToken, report, false, r.ObservedAt); err == nil {
		t.Fatal("evidence storage failure did not roll back report")
	}
	h, err = s.NodeRenderedConfigHistory(ctx, id, at.Add(2*time.Second))
	if err != nil || h.Current.ReceiptSequence != 1 {
		t.Fatal(h, err)
	}
}
