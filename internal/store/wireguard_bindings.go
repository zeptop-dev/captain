package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/wg"
	"github.com/zeptop-dev/captain/internal/domain"
)

// WireGuardBindings includes the legacy owned entry and explicitly attached entries.
func (s *Store) WireGuardBindings(ctx context.Context, id string) ([]domain.Inbound, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id IN (SELECT inbound_id FROM wireguard_inbound_links WHERE id=?) ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Inbound{}
	for rows.Next() {
		ib, e := scanInbound(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *ib)
	}
	return out, rows.Err()
}

// SetWireGuardBinding serializes binding, inbound routing and connection version.
// Callers hold Topology while checking runtime capabilities and invalidating state.
func (s *Store) SetWireGuardBinding(ctx context.Context, exitID int64, id string, version, inboundID int64, attach bool, enabled ...bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	l, err := scanWireGuard(tx.QueryRowContext(ctx, "SELECT "+wireGuardColumns+" FROM node_wireguard_links WHERE id=? AND exit_id=?", id, exitID))
	if err != nil {
		return err
	}
	if l.Version != version {
		return ErrReverseConflict
	}
	ib, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", inboundID))
	if err != nil {
		return err
	}
	if ib.NodeID != l.TransitID || ib.ID == l.UserInboundID || ib.Settings.Reverse != nil {
		return errors.New("inbound cannot be rebound from its managed owner")
	}
	wasEnabled := ib.Enabled
	if attach {
		if len(enabled) > 0 {
			ib.Enabled = enabled[0]
		}
		if ib.Settings.WireGuardSlot != 0 {
			return errors.New("inbound already has a WireGuard connection")
		}
		if !reflect.DeepEqual(ib.Settings.PrivateAccess.EffectiveRules(), l.PrivateAccess.EffectiveRules()) {
			return errors.New("shared WireGuard inbounds must have identical private access")
		}
		if ib.Core == "" {
			return errors.New("select an explicit core before binding WireGuard")
		}
		ib.Settings.WireGuardSlot = l.Slot
		node, e := loadLineNode(ctx, tx, ib.NodeID, 0)
		if e != nil {
			return e
		}
		if ib.Protocol == spec.Mieru && ib.Core == "singbox-extended" && node.node.MitaQuotas {
			return errors.New("Extended Mieru cannot retain native mita quotas")
		}
		if ib.Enabled && !wasEnabled {
			listen := ib.Listen
			if listen == "" && ib.IngressID != nil && node.ingresses[*ib.IngressID] != nil {
				listen = node.ingresses[*ib.IngressID].BindIP
			}
			for _, candidate := range lineInboundBinds(ib, listen) {
				for _, existing := range node.binds {
					if candidate.port == existing.port && lineProtocolsOverlap(candidate.protocol, existing.protocol) && lineListenOverlap(candidate.listen, existing.listen) {
						return errors.New("enabling the inbound overlaps an existing listener")
					}
				}
			}
		}

		if err = validateWireGuardBinding(l, *ib); err != nil {
			return err
		}
		capability := spec.CapabilitiesForCore(ib.Core)
		capability.WireGuardDevices = true
		if !capability.Supports(ib.Spec()) {
			return errors.New("core does not support this WireGuard inbound")
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO wireguard_bindings(inbound_id,link_id) VALUES(?,?)", inboundID, id); err != nil {
			return err
		}
	} else {
		if ib.Settings.WireGuardSlot != l.Slot {
			return ErrReverseConflict
		}
		result, e := tx.ExecContext(ctx, "DELETE FROM wireguard_bindings WHERE inbound_id=? AND link_id=?", inboundID, id)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrReverseConflict
		}
		// Detaching never activates an ordinary direct exit implicitly. The admin
		// must explicitly re-enable the entry after reviewing its new route.
		ib.Settings.WireGuardSlot = 0
		ib.Enabled = false
	}
	if err = saveReverseInbound(ctx, tx, ib); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_wireguard_links SET version=version+1 WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveWireGuardTransport creates an independent tunnel without an owned entry.
func (s *Store) SaveWireGuardTransport(ctx context.Context, l *WireGuardLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if l.ID == "" || l.ExitID == l.TransitID || l.UserInboundID != 0 || l.EntryID != 0 {
		return errors.New("invalid independent transport")
	}
	for _, id := range []int64{l.ExitID, l.TransitID} {
		n, e := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeCols+" FROM nodes WHERE id=?", id))
		if e != nil {
			return e
		}
		if !NodeProxyAllowed(n) {
			return spec.ErrMonitorOnly
		}
	}

	if l.Version > 0 {
		old, e := scanWireGuard(tx.QueryRowContext(ctx, "SELECT "+wireGuardColumns+" FROM node_wireguard_links WHERE id=?", l.ID))
		if e != nil {
			return e
		}
		if old.Version != l.Version || old.UserInboundID != 0 || old.TransitID != l.TransitID || old.ExitID != l.ExitID {
			return ErrReverseConflict
		}
		l.Slot, l.TransitKey, l.ExitKey = old.Slot, old.TransitKey, old.ExitKey
		if !reflect.DeepEqual(old.PrivateAccess.EffectiveRules(), l.PrivateAccess.EffectiveRules()) {
			var count int
			if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM wireguard_bindings WHERE link_id=?", l.ID).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return errors.New("detach all inbounds before changing private access")
			}
		}
	} else {
		for _, id := range []int64{l.ExitID, l.TransitID} {
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_wireguard_links WHERE transit_id=? OR exit_id=?", id, id).Scan(&n); err != nil {
				return err
			}
			if n >= 64 {
				return errors.New("at most 64 WireGuard connections per node")
			}
		}
		l.Slot = 1
		l.TransitKey, _, err = wg.Keypair()
		if err != nil {
			return err
		}
		l.ExitKey, _, err = wg.Keypair()
		if err != nil {
			return err
		}
	}
	if _, err = l.Config(false, l.PrivateAccess); err != nil {
		return err
	}
	if _, err = l.Config(true, l.PrivateAccess); err != nil {
		return err
	}
	if err = checkRolloutWireGuardTunnelTx(ctx, tx, l.TransitID, l.ListenPort, l.Enabled); err != nil {
		return err
	}
	if l.Enabled {
		// Hide only this independent transport's previous reservation inside the
		// transaction while checking the proposed listener against every other one.
		if l.Version > 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE node_wireguard_links SET enabled=0 WHERE id=?", l.ID); err != nil {
				return err
			}
		}
		node, e := loadLineNode(ctx, tx, l.TransitID, 0)
		if e != nil {
			return e
		}
		for _, b := range node.binds {
			if b.port == l.ListenPort && lineProtocolsOverlap(b.protocol, "udp") {
				return errors.New("WireGuard tunnel port overlaps an existing listener")
			}
		}
	}

	raw, err := json.Marshal(l.PrivateAccess)
	if err != nil {
		return err
	}
	if l.Version == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO node_wireguard_links(id,exit_id,transit_id,transit_key,exit_key,endpoint,listen_port,mtu,domain_preference,auto_forwarding,name,enabled,private_access_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, l.ID, l.ExitID, l.TransitID, l.TransitKey, l.ExitKey, l.Endpoint, l.ListenPort, l.MTU, l.DomainPreference, boolInt(l.AutoForwarding), l.Name, boolInt(l.Enabled), string(raw))
		if e != nil {
			return e
		}
		slot, e := res.LastInsertId()
		if e != nil {
			return e
		}
		l.Slot = int(slot)
		l.Version = 1
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE node_wireguard_links SET endpoint=?,listen_port=?,mtu=?,domain_preference=?,auto_forwarding=?,name=?,enabled=?,private_access_json=?,version=version+1 WHERE id=?`, l.Endpoint, l.ListenPort, l.MTU, l.DomainPreference, boolInt(l.AutoForwarding), l.Name, boolInt(l.Enabled), string(raw), l.ID)
		if err != nil {
			return err
		}
		l.Version++
	}
	if err = refreshLinesForNodeTx(ctx, tx, l.TransitID); err != nil {
		return err
	}
	return tx.Commit()
}

func checkWireGuardLandingTx(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	if ib.Settings.WireGuardSlot == 0 {
		return nil
	}
	var exitID int64
	var enabled bool
	err := tx.QueryRowContext(ctx, "SELECT w.exit_id,COALESCE(i.enabled,w.enabled) FROM wireguard_inbound_links w LEFT JOIN inbounds i ON i.id=w.user_inbound_id WHERE w.inbound_id=? AND w.slot=?", ib.ID, ib.Settings.WireGuardSlot).Scan(&exitID, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTopologyUnsupported
	}
	if err != nil {
		return err
	}
	if !enabled {
		return errors.New("WireGuard landing transport is disabled")
	}
	_, err = loadLineNode(ctx, tx, exitID, 0)
	return err
}

func validateWireGuardBinding(l WireGuardLink, ib domain.Inbound) error {
	c, err := l.Config(false, l.PrivateAccess)
	if err != nil {
		return err
	}
	n := spec.Node{WireGuardLinks: []spec.WireGuardLink{c}, Inbounds: []spec.Inbound{ib.Spec()}}
	return n.ValidateWireGuardLinks()
}
