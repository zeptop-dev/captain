package store

import (
	"context"
	"errors"
	"os"
	"time"
)

var ErrMaintenanceBusy = errors.New("monitoring maintenance already running")

type MonitorStorage struct {
	DatabaseBytes   int64           `json:"database_bytes"`
	WALBytes        int64           `json:"wal_bytes"`
	SHMBytes        int64           `json:"shm_bytes"`
	FreeBytes       int64           `json:"free_bytes"`
	MonitoringBytes *int64          `json:"monitoring_bytes"`
	SizeKnown       bool            `json:"size_known"`
	TableSizesKnown bool            `json:"table_sizes_known"`
	Retention       RetentionStatus `json:"retention"`
	At              int64           `json:"at"`
}

func (s *Store) MonitorStorage(ctx context.Context) (MonitorStorage, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := MonitorStorage{Retention: s.RetentionStatus(), At: time.Now().Unix()}
	var pages, free, pageSize int64
	if e := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); e != nil {
		return out, e
	}
	if e := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&free); e != nil {
		return out, e
	}
	if e := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); e != nil {
		return out, e
	}
	out.DatabaseBytes = pages * pageSize
	out.FreeBytes = free * pageSize
	rows, e := s.db.QueryContext(ctx, "PRAGMA database_list")
	if e != nil {
		return out, e
	}
	var file string
	for rows.Next() {
		var seq int
		var name, path string
		if e = rows.Scan(&seq, &name, &path); e != nil {
			rows.Close()
			return out, e
		}
		if name == "main" {
			file = path
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if file != "" {
		out.SizeKnown = true
		for suffix, dst := range map[string]*int64{"": &out.DatabaseBytes, "-wal": &out.WALBytes, "-shm": &out.SHMBytes} {
			info, e := os.Stat(file + suffix)
			if e == nil {
				*dst = info.Size()
			} else if !os.IsNotExist(e) {
				out.SizeKnown = false
			}
		}
	}
	// dbstat may not be available on all SQLite builds. Unknown is not zero.
	var size int64
	if e := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name IN (SELECT name FROM sqlite_master WHERE (type='table' AND (name LIKE 'node_probe_%' OR name LIKE 'node_ping_%' OR name IN ('node_stats','node_resource_stats','monitor_intervals','monitor_incidents','node_route_events','node_route_states','node_network_baselines'))) OR (type='index' AND (tbl_name LIKE 'node_probe_%' OR tbl_name LIKE 'node_ping_%' OR tbl_name IN ('node_stats','node_resource_stats','monitor_intervals','monitor_incidents','node_route_events','node_route_states','node_network_baselines'))))`).Scan(&size); e == nil {
		out.MonitoringBytes = &size
		out.TableSizesKnown = true
	}
	return out, nil
}

type MonitorMaintenance struct {
	Retention      RetentionStatus `json:"retention"`
	CheckpointBusy int             `json:"checkpoint_busy"`
	LogPages       int             `json:"log_pages"`
	Checkpointed   int             `json:"checkpointed"`
	At             int64           `json:"at"`
}

func (s *Store) MaintainMonitoring(ctx context.Context) (MonitorMaintenance, error) {
	if !s.maintenance.TryLock() {
		return MonitorMaintenance{}, ErrMaintenanceBusy
	}
	defer s.maintenance.Unlock()
	if e := s.PruneStats(ctx, time.Now()); e != nil {
		return MonitorMaintenance{Retention: s.RetentionStatus()}, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	out := MonitorMaintenance{Retention: s.RetentionStatus(), At: time.Now().Unix()}
	// PASSIVE does not wait for readers or rewrite the entire shared business DB.
	e := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&out.CheckpointBusy, &out.LogPages, &out.Checkpointed)
	return out, e
}
