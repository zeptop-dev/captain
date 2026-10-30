package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RetentionStatus describes the latest bounded monitoring-history cleanup in
// this process. Backlog is conservative: a full batch means possible backlog,
// not a full COUNT scan. Restart and unvisited tables remain unknown.
type RetentionStatus struct {
	LastRun         *time.Time `json:"last_run,omitempty"`
	LastSuccess     *time.Time `json:"last_success,omitempty"`
	DurationMS      int64      `json:"duration_ms"`
	Rows            int64      `json:"rows"`
	Tables          int        `json:"tables"`
	Backlog         string     `json:"backlog"` // unknown, possible, clear
	BudgetExhausted bool       `json:"budget_exhausted"`
	Errors          int        `json:"errors"`
}
type retentionState struct {
	run    sync.Mutex
	mu     sync.Mutex
	next   int
	status RetentionStatus
}

func (s *Store) RetentionStatus() RetentionStatus {
	s.retention.mu.Lock()
	defer s.retention.mu.Unlock()
	out := s.retention.status
	if out.Backlog == "" {
		out.Backlog = "unknown"
	}
	return out
}

type retentionTask struct {
	table, where string
	args         []any
}

func monitoringRetentionTasks(at time.Time) []retentionTask {
	var tasks []retentionTask
	add := func(table, where string, args ...any) { tasks = append(tasks, retentionTask{table, where, args}) }
	for _, r := range []struct {
		res  string
		keep time.Duration
	}{{"r", 2 * time.Hour}, {"m", 48 * time.Hour}, {"h", 60 * 24 * time.Hour}, {"d", 730 * 24 * time.Hour}} {
		add("node_probe_metric_stats", "res=? AND ts<?", r.res, at.Add(-r.keep).Unix())
		if r.res != "r" {
			for _, table := range []string{"node_stats", "node_ping_stats", "node_ping_series_stats"} {
				add(table, "res=? AND ts<?", r.res, at.Add(-r.keep).Unix())
			}
		}
	}
	for _, table := range []string{"node_ping_cursors", "node_ping_series_cursors"} {
		add(table, "seen_at<?", at.Add(-7*24*time.Hour).Unix())
	}
	for _, r := range []struct {
		res  string
		keep time.Duration
	}{{"m", 24 * time.Hour}, {"h", 14 * 24 * time.Hour}, {"d", 90 * 24 * time.Hour}} {
		add("node_resource_stats", "res=? AND ts<?", r.res, at.Add(-r.keep).Unix())
	}
	add("node_jobs", "kind='network_diagnostic' AND route_check=0 AND created_at<?", at.Add(-24*time.Hour).Unix())
	add("monitor_intervals", "ends_at<?", at.Add(-90*24*time.Hour).Unix())
	add("monitor_incidents", "ended_at<?", at.Add(-180*24*time.Hour).Unix())
	add("monitor_windows", "ends_at<? OR canceled_at<?", at.Add(-180*24*time.Hour).Unix(), at.Add(-180*24*time.Hour).Unix())
	add("node_traffic_history", "received_at<?", at.Add(-trafficHistoryDays*24*time.Hour).UnixMilli())
	return tasks
}

// PruneStats retains the existing cutoff semantics. Each eligible table and
// resolution gets at most one 500-row statement; the whole round has a two
// second cancellation budget. Statements release SQLite's single connection
// between batches. Rotation prevents an early large table starving later ones.
func (s *Store) PruneStats(ctx context.Context, at time.Time) error {
	return s.pruneStatsBounded(ctx, at, 500, 2*time.Second)
}
func (s *Store) pruneStatsBounded(parent context.Context, at time.Time, batch int, budget time.Duration) error {
	s.retention.run.Lock()
	defer s.retention.run.Unlock()
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	tasks := monitoringRetentionTasks(at)
	status := RetentionStatus{LastRun: &at, Backlog: "clear", LastSuccess: s.RetentionStatus().LastSuccess}
	start := s.retention.next % len(tasks)
	var errs []error
	for n := 0; n < len(tasks); n++ {
		index := (start + n) % len(tasks)
		if ctx.Err() != nil {
			status.BudgetExhausted = true
			status.Backlog = "unknown"
			break
		}
		task := tasks[index]
		// Identifiers/predicates come only from the fixed registry above.
		args := append(append([]any{}, task.args...), batch)
		result, err := s.db.ExecContext(ctx, "DELETE FROM "+task.table+" WHERE rowid IN (SELECT rowid FROM "+task.table+" WHERE "+task.where+" LIMIT ?)", args...)
		s.retention.next = (index + 1) % len(tasks)
		if err != nil {
			if ctx.Err() != nil {
				status.BudgetExhausted = true
				status.Backlog = "unknown"
				break
			}
			errs = append(errs, fmt.Errorf("prune %s: %w", task.table, err))
			status.Errors++
			status.Backlog = "unknown"
			continue
		}
		rows, err := result.RowsAffected()
		if err != nil {
			errs = append(errs, err)
			status.Errors++
			status.Backlog = "unknown"
			continue
		}
		status.Tables++
		status.Rows += rows
		if rows == int64(batch) && status.Backlog == "clear" {
			status.Backlog = "possible"
		}
	}
	if status.Errors == 0 && !status.BudgetExhausted {
		status.LastSuccess = &at
	}
	status.DurationMS = time.Since(started).Milliseconds()
	s.retention.mu.Lock()
	s.retention.status = status
	s.retention.mu.Unlock()
	if parent.Err() != nil {
		errs = append(errs, parent.Err())
	}
	return errors.Join(errs...)
}
