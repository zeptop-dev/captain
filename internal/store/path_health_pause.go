package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Desired control has no observation expiry. Runtime control is only evidence
// while the independently accepted full report is fresh; heartbeats and health
// receipt retries cannot extend that clock.
func pathHealthNodePausedTx(ctx context.Context, tx *sql.Tx, nodeID int64, at time.Time) (bool, error) {
	control, err := serviceControlTx(ctx, tx, nodeID)
	if err != nil {
		return false, err
	}
	if control.effective().Mode == spec.ServiceControlPause {
		return true, nil
	}
	var raw string
	var reported sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT runtime_report_json,runtime_reported_at FROM nodes WHERE id=?`, nodeID).Scan(&raw, &reported); err != nil {
		return false, err
	}
	if !reported.Valid {
		return false, nil
	}
	observed := time.Unix(reported.Int64, 0)
	if observed.Before(at.Add(-3*time.Minute)) || observed.After(at.Add(30*time.Second)) {
		return false, nil
	}
	var runtime NodeRuntime
	return json.Unmarshal([]byte(raw), &runtime) == nil && runtime.ServiceControl != nil && runtime.ServiceControl.Mode == spec.ServiceControlPause, nil
}
