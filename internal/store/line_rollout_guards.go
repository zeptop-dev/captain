package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/zeptop-dev/bosun/pkg/spec"
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
	if err := checkRolloutLandingMutationTx(ctx, tx, ib); err != nil {
		return err
	}
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
	if err := checkRolloutNodeTopologyTx(ctx, tx, nodeID); err != nil {
		return err
	}
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

// Future landings are not yet in forward_lines. Protect both immutable sides
// of every active plan instead of relying on current line ownership.
func rolloutInboundReferencedTx(ctx context.Context, q addressQuery, id int64) (bool, error) {
	var used bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollouts WHERE status IN ('running','paused') AND (json_extract(before_json,'$.target_inbound_id')=? OR json_extract(after_json,'$.target_inbound_id')=?))`, id, id).Scan(&used)
	return used, err
}
func checkRolloutLandingMutationTx(ctx context.Context, tx *sql.Tx, next *domain.Inbound) error {
	if lineRolloutWrite(ctx) || next.ID == 0 {
		return nil
	}
	used, err := rolloutInboundReferencedTx(ctx, tx, next.ID)
	if err != nil || !used {
		return err
	}
	before, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", next.ID))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rolloutLandingStructure(before), rolloutLandingStructure(next)) {
		return fmt.Errorf("%w: landing structure is reserved by an active release", ErrLineRolloutConflict)
	}
	return nil
}

// Credentials and authorization remain current. Only listener and public
// handshake structure participate; certificates, PSKs and Users do not.
func rolloutLandingStructure(ib *domain.Inbound) any {
	var tlsMode spec.TLSMode
	var serverName string
	var alpn []string
	if ib.Settings.TLS != nil {
		tlsMode = ib.Settings.TLS.Mode
		serverName = ib.Settings.TLS.ServerName
		alpn = append([]string(nil), ib.Settings.TLS.ALPN...)
	}
	var transport spec.Transport
	if ib.Settings.Transport != nil {
		transport = *ib.Settings.Transport
	}
	transport.Type = ib.Settings.TransportType()
	return struct {
		NodeID              int64
		Tag                 string
		Protocol            spec.Protocol
		Listen              string
		Port                int
		Core                string
		Enabled             bool
		IngressID           *int64
		TLSMode             spec.TLSMode
		ServerName          string
		ALPN                []string
		Transport           spec.Transport
		ShadowTLS           *spec.ShadowTLS
		AcceptProxyProtocol bool
		MieruTransport      string
	}{ib.NodeID, ib.Tag, ib.Protocol, ib.Listen, ib.Port, ib.Core, ib.Enabled, ib.IngressID, tlsMode, serverName, alpn, transport, ib.Settings.ShadowTLS, ib.Settings.AcceptProxyProtocol, ib.Settings.MieruTransport}
}
func checkRolloutNodeTopologyTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	if lineRolloutWrite(ctx) {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM line_rollouts WHERE status IN ('running','paused') AND EXISTS(SELECT 1 FROM json_each(dependencies_json) WHERE json_extract(value,'$.node_id')=?)`, nodeID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, err := readLineRolloutTx(ctx, tx, id, timeNowRollout())
		if err != nil {
			return err
		}
		if err = lineRolloutTopologyTx(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

func rolloutIngressReferencedTx(ctx context.Context, q addressQuery, id int64) (bool, error) {
	var used bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbounds i JOIN line_rollouts p ON i.id=json_extract(p.before_json,'$.target_inbound_id') OR i.id=json_extract(p.after_json,'$.target_inbound_id') WHERE i.ingress_id=? AND p.status IN ('running','paused'))`, id).Scan(&used)
	return used, err
}
func checkRolloutIngressMutationTx(ctx context.Context, tx *sql.Tx, next *Ingress) error {
	used, err := rolloutIngressReferencedTx(ctx, tx, next.ID)
	if err != nil || !used {
		return err
	}
	old, err := scanIngress(tx.QueryRowContext(ctx, "SELECT "+ingressCols+" FROM ingresses WHERE id=?", next.ID))
	if err != nil {
		return err
	}
	structure := func(v *Ingress) Ingress {
		x := *v
		x.Name = ""
		x.CreatedAt = old.CreatedAt
		x.UpdatedAt = old.UpdatedAt
		x.NodeID = old.NodeID
		x.ReservedPorts = append([]int(nil), x.ReservedPorts...)
		x.PortMappings = append([]spec.PortMapping(nil), x.PortMappings...)
		return x
	}
	if !reflect.DeepEqual(structure(old), structure(next)) {
		return fmt.Errorf("%w: landing ingress is reserved by an active release", ErrLineRolloutConflict)
	}
	return nil
}
