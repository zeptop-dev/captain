package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"testing"
	"time"
)

func coldPrepare(t *testing.T, s *Store, id int64, session string, at time.Time) *agentproto.ConfigDelivery {
	t.Helper()
	fs, err := s.NodeForwards(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var wire []spec.Forward
	for _, f := range fs {
		f.IngressID = ""
		wire = append(wire, f.Forward)
	}
	d, err := s.PrepareNodeConfigDeliveryForLines(context.Background(), id, configTestToken, session, "cfg-one", "state-one", wire, at)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestColdReviewPartialSupersedeNotApplied(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	for _, n := range ns {
		if _, err := s.db.Exec(`UPDATE nodes SET token_hash=? WHERE id=?`, configTestToken+string(rune(n.ID)), n.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Receipt helper uses one token; nodes need not have globally shared tokens.
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	first, err := s.LatestLineChangeID(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].Port++
	if err = s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Second)
	for _, n := range ns[1:3] {
		fs, _ := s.NodeForwards(ctx, n.ID)
		var wire []spec.Forward
		for _, f := range fs {
			wire = append(wire, f.Forward)
		}
		tok := configTestToken + string(rune(n.ID))
		d, e := s.PrepareNodeConfigDeliveryForLines(ctx, n.ID, tok, configSessionA, "cfg-one", "state-one", wire, at)
		if e != nil {
			t.Fatal(e)
		}
		r := configReceipt(d, "cfg-one", "state-one", 1, at)
		if _, e = s.RecordNodeReport(ctx, n.ID, tok, agentproto.Report{AppliedRevision: r.StateRevision, ConfigReceipt: r}, false, at); e != nil {
			t.Fatal(e)
		}
	}
	got, err := s.LineChangeByID(ctx, first, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "applied" {
		t.Fatalf("partially superseded original change reports applied: %+v", got)
	}
}

func TestColdReviewCandidateDoesNotOverwriteActiveTarget(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	if _, err := s.db.Exec(`UPDATE nodes SET token_hash=? WHERE id=?`, configTestToken, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	first, err := s.LatestLineChangeID(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Second)
	active := coldPrepare(t, s, ns[0].ID, configSessionA, at)
	recordConfig(t, s, ns[0].ID, configReceipt(active, "cfg-one", "state-one", 1, at), at)
	before, err := s.LineChangeByID(ctx, first, at)
	if err != nil {
		t.Fatal(err)
	}
	if before.Targets[0].Status != "applied" {
		t.Fatal(before)
	}
	coldPrepare(t, s, ns[0].ID, configSessionB, at.Add(time.Second))
	recordConfig(t, s, ns[0].ID, configReceipt(active, "cfg-one", "state-one", 2, at.Add(2*time.Second)), at.Add(2*time.Second))
	got, err := s.LineChangeByID(ctx, first, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Targets[0].Status != "applied" || got.Targets[0].DeliveryID != active.ID {
		t.Fatalf("unactivated candidate stole active target evidence: %+v", got.Targets[0])
	}
}
