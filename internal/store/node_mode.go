package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func NodeProxyAllowed(n *domain.Node) bool {
	return n.Mode.Effective() == spec.NodeFull && n.ReportedMode != spec.NodeMonitor && (!n.ModeManaged || (n.ReportedMode == spec.NodeFull && n.ReportedModeRevision == n.ModeRevision))
}

func (s *Store) RequireProxyNode(ctx context.Context, id int64) error {
	n, err := s.NodeByID(ctx, id)
	if err != nil {
		return err
	}
	if !NodeProxyAllowed(n) {
		return spec.ErrMonitorOnly
	}
	return nil
}

// SetNodeMode is called under Topology, the same lock as topology writers.
// The transaction checks disabled resources as well as active ones.
func (s *Store) SetNodeMode(ctx context.Context, id int64, mode, expected spec.NodeMode, expectedRevision uint64) error {
	if mode == "" {
		return errors.New("node mode is required")
	}
	if err := mode.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n, err := scanNode(tx.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id = ?`, id))
	if err != nil {
		return err
	}
	if n.Mode != expected || n.ModeRevision != expectedRevision {
		return errors.New("node mode changed; refresh before trying again")
	}
	if n.Mode == mode {
		return nil
	}
	if n.Paired && (n.ReportedMode == "" || n.ModeReportedAt == nil || time.Since(*n.ModeReportedAt) > 3*time.Minute) {
		return errors.New("mode changes require an online node reporting mode support; upgrade bosun first")
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_jobs WHERE node_id = ? AND done_at IS NULL`, id).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return errors.New("wait for pending node jobs before changing mode")
	}
	if mode == spec.NodeMonitor {
		control, err := serviceControlTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if control.state.Policy.Action != "alert" || control.effective().Mode != "normal" {
			return errors.New("restore service and disable automatic traffic protection before switching to monitor-only mode")
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM inbounds WHERE node_id = ?) + (SELECT COUNT(*) FROM node_reverse_links WHERE exit_id = ? OR transit_id = ?) + (SELECT COUNT(*) FROM node_wireguard_links WHERE exit_id = ? OR transit_id = ?)`, id, id, id, id, id).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("remove all inbounds (including disabled ones) and reverse connections before switching to monitor-only mode")
		}
		var forwards, outs, routes, dns, overrides, upstreams, def string
		if err = tx.QueryRowContext(ctx, `SELECT forwards_json,outbounds_json,routes_json,dns_json,overrides_json,egress_upstreams_json,default_outbound FROM nodes WHERE id = ?`, id).Scan(&forwards, &outs, &routes, &dns, &overrides, &upstreams, &def); err != nil {
			return err
		}
		for _, raw := range []string{forwards, outs, routes, dns, upstreams} {
			var values []json.RawMessage
			if err = json.Unmarshal([]byte(raw), &values); err != nil {
				return err
			}
			if len(values) > 0 {
				return errors.New("remove forwarding, routing and egress policies before switching to monitor-only mode")
			}
		}
		var ov map[string]json.RawMessage
		if err = json.Unmarshal([]byte(overrides), &ov); err != nil {
			return err
		}
		if len(ov) > 0 || def != "" || n.DecoyEnabled || n.UserSpeedLimitMbps != 0 || n.MitaQuotas || n.EgressByIngress {
			return errors.New("clear proxy overrides, decoy and user policies before switching to monitor-only mode")
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET node_mode = ?, mode_managed = 1, mode_revision = mode_revision + 1, mode_error = '', updated_at = ? WHERE id = ?`, mode, now(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReportNodeMode(ctx context.Context, id int64, tokenHash string, mode spec.NodeMode, revision uint64, modeError string) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	// Missing support from an older binary invalidates the previous observation.
	if len(modeError) > 512 {
		modeError = modeError[:512]
	}
	// A request authenticated just before re-pairing must not acknowledge the
	// new identity's mode after the old token has been revoked.
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET reported_mode = ?, reported_mode_revision = CASE WHEN ? = '' THEN reported_mode_revision ELSE ? END, mode_reported_at = ?, mode_error = ? WHERE id = ? AND token_hash = ? AND (reported_mode_revision <= ? OR ? = '')`, mode, mode, revision, now(), modeError, id, tokenHash, revision, mode)
	return err
}

func (s *Store) NodeByPairCode(ctx context.Context, code string) (*domain.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE pair_code = ? AND pair_code_expires_at > ? AND token_hash IS NULL`, code, now()))
}
