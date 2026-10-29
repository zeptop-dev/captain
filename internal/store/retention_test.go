package store

import (
	"context"
	"testing"
	"time"
)

func TestMonitoringRetentionBatchesPreserveCurrentAndOpenData(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	if s.RetentionStatus().Backlog != "unknown" {
		t.Fatal("fresh process claimed no backlog")
	}
	old := at.Add(-800 * 24 * time.Hour).Unix()
	for i := 0; i < 7; i++ {
		if _, err := s.db.Exec(`INSERT INTO node_stats(node_id,res,ts) VALUES(?,?,?)`, id, "m", old+int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO node_stats(node_id,res,ts) VALUES(?,?,?)`, id, "m", at.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO monitor_incidents(node_id,kind,started_at,updated_at) VALUES(?,?,?,?)`, id, "offline", old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneStatsBounded(ctx, at, 3, time.Second); err != nil {
		t.Fatal(err)
	}
	st := s.RetentionStatus()
	if st.Rows != 3 || st.Backlog != "possible" || st.LastSuccess == nil {
		t.Fatalf("batch status: %+v", st)
	}
	for range 3 {
		if err := s.pruneStatsBounded(ctx, at, 3, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if s.RetentionStatus().Backlog != "clear" {
		t.Fatal(s.RetentionStatus())
	}
	for table, want := range map[string]int{"node_stats": 1, "monitor_incidents": 1} {
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
}
func TestMonitoringRetentionBudgetAndIndependentFailure(t *testing.T) {
	s, id, at := monitorRig(t)
	ctx := context.Background()
	if err := s.pruneStatsBounded(ctx, at, 1, 0); err != nil {
		t.Fatal(err)
	}
	st := s.RetentionStatus()
	if !st.BudgetExhausted || st.LastSuccess != nil || st.Backlog != "unknown" {
		t.Fatal(st)
	}
	// A failed early table must not hide or starve a later eligible table.
	if _, err := s.db.Exec(`DROP TABLE node_probe_metric_stats`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO monitor_intervals(node_id,starts_at,ends_at,online) VALUES(?,?,?,?)`, id, at.Add(-200*24*time.Hour).Unix(), at.Add(-199*24*time.Hour).Unix(), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneStatsBounded(ctx, at, 3, time.Second); err == nil {
		t.Fatal("missing error")
	}
	st = s.RetentionStatus()
	if st.Rows != 1 || st.Errors != 4 || st.LastSuccess != nil || st.Backlog != "unknown" {
		t.Fatal(st)
	}
}

func TestMonitoringRetentionRotatesAfterInterruptedStatement(t *testing.T) {
	s, _, at := monitorRig(t)
	ctx := context.Background()
	// Hold the only connection so a statement reaches its cancellation budget.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for want := 1; want <= 2; want++ {
		if err := s.pruneStatsBounded(ctx, at, 3, 10*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		st := s.RetentionStatus()
		if s.retention.next != want || !st.BudgetExhausted || st.Rows != 0 || st.Backlog != "unknown" {
			t.Fatalf("rotation %d: next=%d status=%+v", want, s.retention.next, st)
		}
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneStatsBounded(ctx, at, 3, time.Second); err != nil {
		t.Fatal(err)
	}
	if s.RetentionStatus().Backlog != "clear" || s.retention.next != 2 {
		t.Fatal("rotated pass did not finish", s.RetentionStatus())
	}
}
