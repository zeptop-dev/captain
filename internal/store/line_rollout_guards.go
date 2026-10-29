package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/zeptop-dev/captain/internal/domain"
)

type lineRolloutReservation struct {
	lineID  int64
	forward LineChangeForward
}

func lineRolloutReservationsTx(ctx context.Context, tx *sql.Tx, nodeID, excludeLine int64) ([]lineRolloutReservation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.line_id,n.before_json,n.after_json FROM line_rollout_nodes n JOIN line_rollouts p ON p.id=n.plan_id WHERE n.node_id=? AND p.line_id<>? AND p.status IN ('running','paused')`, nodeID, excludeLine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lineRolloutReservation{}
	for rows.Next() {
		var lineID int64
		var b, a string
		if err = rows.Scan(&lineID, &b, &a); err != nil {
			return nil, err
		}
		for _, raw := range []string{b, a} {
			var fs []LineChangeForward
			if err = json.Unmarshal([]byte(raw), &fs); err != nil {
				return nil, err
			}
			for _, f := range fs {
				out = append(out, lineRolloutReservation{lineID, f})
			}
		}
	}
	return out, rows.Err()
}
func checkRolloutInboundTx(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	if !ib.Enabled {
		return nil
	}
	reservations, err := lineRolloutReservationsTx(ctx, tx, ib.NodeID, 0)
	if err != nil {
		return err
	}
	listen := ib.Listen
	if listen == "" && ib.IngressID != nil {
		if err = tx.QueryRowContext(ctx, `SELECT bind_ip FROM ingresses WHERE id=? AND node_id=?`, *ib.IngressID, ib.NodeID).Scan(&listen); err != nil {
			return err
		}
	}
	for _, b := range lineInboundBinds(ib, listen) {
		for _, r := range reservations {
			if b.protocol == "tcp" && b.port == r.forward.Port && lineListenOverlap(b.listen, r.forward.Listen) {
				return fmt.Errorf("%w: TCP port %d is reserved by a line plan", ErrLineRolloutConflict, b.port)
			}
		}
	}
	return nil
}
func checkRolloutForwardsTx(ctx context.Context, tx *sql.Tx, nodeID int64, fs []NodeForward) error {
	reservations, err := lineRolloutReservationsTx(ctx, tx, nodeID, 0)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.Listen == "" && f.IngressID != "" {
			e := tx.QueryRowContext(ctx, `SELECT bind_ip FROM ingresses WHERE id=? AND node_id=?`, f.IngressID, nodeID).Scan(&f.Listen)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		for _, r := range reservations {
			if f.LineID == r.lineID {
				continue
			}
			for _, protocol := range lineProtocols(f.Protocol) {
				if protocol == "tcp" && f.Port == r.forward.Port && lineListenOverlap(f.Listen, r.forward.Listen) {
					return fmt.Errorf("%w: TCP port %d is reserved by a line plan", ErrLineRolloutConflict, f.Port)
				}
			}
		}
	}
	return nil
}
func allowRolloutRecompileTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, generated map[int64][]NodeForward, endpoints map[string]LineEndpoint) (bool, error) {
	if lineRolloutWrite(ctx) {
		return false, nil
	}
	active, err := rolloutActiveTx(ctx, tx, l.ID)
	if err != nil || !active {
		return false, err
	}
	stored, err := lineOwnedSnapshotTx(ctx, tx, l.ID)
	if err != nil {
		return false, err
	}
	compiled := map[int64][]LineChangeForward{}
	for id, fs := range generated {
		wire := makeLineRolloutWire(fs)
		compiled[id] = lineChangeProjection(wire)
	}
	if !reflect.DeepEqual(stored, compiled) {
		return false, ErrLineRolloutConflict
	}
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM line_rollouts WHERE line_id=? AND status IN ('running','paused')`, l.ID).Scan(&id); err != nil {
		return false, err
	}
	p, err := readLineRolloutTx(ctx, tx, id, timeNowRollout())
	if err != nil {
		return false, err
	}
	intent := p.Before
	if p.Kind == "create" || p.PublishedAt != nil {
		intent = p.After
	}
	if intent != nil && !sameRolloutEndpoints(intent.Endpoints, endpoints) {
		return false, ErrLineRolloutConflict
	}
	// Access/group/user changes are not structural writes. They must continue
	// without bumping the frozen line version or changing its publication.
	return true, nil
}

// Address indirection can change effective listeners without editing the
// inbound/forward itself. Check the post-mutation view in the same transaction.
func checkRolloutNodeReservationsTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollouts p JOIN line_rollout_nodes n ON n.plan_id=p.id WHERE n.node_id=? AND p.status IN ('running','paused'))`, nodeID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE node_id=? AND enabled=1", nodeID)
	if err != nil {
		return err
	}
	ibs := []*domain.Inbound{}
	for rows.Next() {
		ib, e := scanInbound(rows)
		if e != nil {
			rows.Close()
			return e
		}
		ibs = append(ibs, ib)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, ib := range ibs {
		if err = checkRolloutInboundTx(ctx, tx, ib); err != nil {
			return err
		}
	}
	forwards, err := nodeForwardsTx(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	return checkRolloutForwardsTx(ctx, tx, nodeID, forwards)
}
