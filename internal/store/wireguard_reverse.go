package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/wg"
	"github.com/zeptop-dev/captain/internal/domain"
)

type WireGuardLink struct {
	Slot             int    `json:"slot"`
	ID               string `json:"id"`
	ExitID           int64  `json:"exit_id"`
	TransitID        int64  `json:"transit_id"`
	UserInboundID    int64  `json:"user_inbound_id"`
	EntryID          int64  `json:"entry_id"`
	Version          int64  `json:"version"`
	TransitKey       string `json:"-"`
	ExitKey          string `json:"-"`
	Endpoint         string `json:"endpoint"`
	ListenPort       int    `json:"listen_port"`
	MTU              int    `json:"mtu"`
	DomainPreference string `json:"domain_preference"`
	AutoForwarding   bool   `json:"auto_forwarding"`
}

const wireGuardColumns = "slot,id,exit_id,transit_id,user_inbound_id,entry_id,version,transit_key,exit_key,endpoint,listen_port,mtu,domain_preference,auto_forwarding"

func scanWireGuard(row interface{ Scan(...any) error }) (WireGuardLink, error) {
	var l WireGuardLink
	err := row.Scan(&l.Slot, &l.ID, &l.ExitID, &l.TransitID, &l.UserInboundID, &l.EntryID, &l.Version, &l.TransitKey, &l.ExitKey, &l.Endpoint, &l.ListenPort, &l.MTU, &l.DomainPreference, &l.AutoForwarding)
	return l, wrapNotFound(err)
}
func (s *Store) WireGuardLinks(ctx context.Context, nodeID int64) ([]WireGuardLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+wireGuardColumns+` FROM node_wireguard_links WHERE exit_id=? OR transit_id=? ORDER BY slot`, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WireGuardLink{}
	for rows.Next() {
		l, err := scanWireGuard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (l WireGuardLink) Config(exit bool, grant *spec.PrivateAccess) (spec.WireGuardLink, error) {
	key, peerKey := l.TransitKey, l.ExitKey
	if exit {
		key, peerKey = l.ExitKey, l.TransitKey
	}
	peer, err := wg.PublicKey(peerKey)
	if err != nil {
		return spec.WireGuardLink{}, err
	}
	c := spec.WireGuardLink{Slot: l.Slot, Exit: exit, PrivateKey: key, PeerKey: peer, MTU: l.MTU, PrivateAccess: grant}
	if exit {
		c.Endpoint = l.Endpoint
		c.AutoForwarding = l.AutoForwarding
	} else {
		c.ListenPort = l.ListenPort
		c.DomainPreference = l.DomainPreference
	}
	return c, c.Validate()
}

type WireGuardWrite struct {
	Link  WireGuardLink
	User  domain.Inbound
	Entry domain.Entry
}

// SaveWireGuardLink owns the complete connection transaction. Callers hold
// Topology across prospective listener/policy validation and cache invalidation.
// Store-level identity checks prevent a stale editor from touching other links.
func (s *Store) SaveWireGuardLink(ctx context.Context, w *WireGuardWrite) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	l := w.Link
	ib := w.User
	e := w.Entry
	if l.ID == "" || l.ExitID == l.TransitID || ib.NodeID != l.TransitID {
		return errors.New("invalid WireGuard ownership")
	}
	if l.Version > 0 {
		old, err := scanWireGuard(tx.QueryRowContext(ctx, `SELECT `+wireGuardColumns+` FROM node_wireguard_links WHERE id=?`, l.ID))
		if err != nil {
			return err
		}
		if old.Version != l.Version || old.ExitID != l.ExitID || old.TransitID != l.TransitID || old.UserInboundID != ib.ID || old.EntryID != e.ID || old.Slot != l.Slot {
			return ErrReverseConflict
		}
		l.TransitKey, l.ExitKey = old.TransitKey, old.ExitKey
	} else {
		if ib.ID != 0 || e.ID != 0 || l.Slot != 0 {
			return errors.New("new WireGuard link cannot adopt existing resources")
		}
		for _, nodeID := range []int64{l.ExitID, l.TransitID} {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_wireguard_links WHERE exit_id=? OR transit_id=?`, nodeID, nodeID).Scan(&count); err != nil {
				return err
			}
			if count >= 64 {
				return errors.New("node supports at most 64 WireGuard links")
			}
		}
		// Use a provisional valid slot to validate fields before creating rows.
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
	if _, err := l.Config(false, ib.Settings.PrivateAccess); err != nil {
		return err
	}
	if _, err := l.Config(true, ib.Settings.PrivateAccess); err != nil {
		return err
	}
	ib.Settings.WireGuardSlot = l.Slot
	if err := ib.Spec().Validate(); err != nil {
		return err
	}
	if err := checkRolloutWireGuardTunnelTx(ctx, tx, l.TransitID, l.ListenPort, ib.Enabled); err != nil {
		return err
	}
	if l.Version > 0 {
		// Updating the user inbound recompiles dependent line plans. Expose
		// the proposed tunnel port first, so enabling a disabled link while
		// moving it off a reserved port validates the final transaction view.
		if _, err = tx.ExecContext(ctx, `UPDATE node_wireguard_links SET endpoint=?,listen_port=?,mtu=?,domain_preference=?,auto_forwarding=?,version=version+1 WHERE id=? AND version=?`, l.Endpoint, l.ListenPort, l.MTU, l.DomainPreference, boolInt(l.AutoForwarding), l.ID, l.Version); err != nil {
			return err
		}
		l.Version++
	}
	if err := saveReverseInbound(ctx, tx, &ib); err != nil {
		return err
	}
	if e.ID == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO entries(name,inbound_id,display_host,display_port,rate,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, e.Name, ib.ID, e.DisplayHost, e.DisplayPort, 1, boolInt(e.Enabled), now(), now())
		if err != nil {
			return err
		}
		e.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE entries SET name=?,display_host=?,display_port=?,enabled=?,updated_at=? WHERE id=? AND inbound_id=?`, e.Name, e.DisplayHost, e.DisplayPort, boolInt(e.Enabled), now(), e.ID, ib.ID); err != nil {
			return err
		}
	}
	if l.Version == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO node_wireguard_links(id,exit_id,transit_id,user_inbound_id,entry_id,transit_key,exit_key,endpoint,listen_port,mtu,domain_preference,auto_forwarding) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, l.ID, l.ExitID, l.TransitID, ib.ID, e.ID, l.TransitKey, l.ExitKey, l.Endpoint, l.ListenPort, l.MTU, l.DomainPreference, boolInt(l.AutoForwarding))
		if err != nil {
			return err
		}
		slot, err := result.LastInsertId()
		if err != nil {
			return err
		}
		l.Slot = int(slot)
		l.Version = 1
		ib.Settings.WireGuardSlot = l.Slot
		if err := saveReverseInbound(ctx, tx, &ib); err != nil {
			return err
		}
	}
	l.UserInboundID, l.EntryID = ib.ID, e.ID
	e.InboundID = ib.ID
	if err := tx.Commit(); err != nil {
		return err
	}
	w.Link, w.User, w.Entry = l, ib, e
	return nil
}
func (s *Store) DeleteWireGuardLink(ctx context.Context, exitID int64, id string, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inboundID int64
	err = tx.QueryRowContext(ctx, `SELECT user_inbound_id FROM node_wireguard_links WHERE id=? AND exit_id=? AND version=?`, id, exitID, version).Scan(&inboundID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReverseConflict
	}
	if err != nil {
		return err
	}
	if err = checkReverseInboundRemoval(ctx, tx, inboundID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM node_wireguard_links WHERE id=? AND exit_id=? AND version=?`, id, exitID, version); err != nil {
		return err
	}
	return tx.Commit()
}

// Reports have their own clock; resource heartbeats must not refresh an old
// transport capability. Omitted reports clear availability for downgraded nodes.
func (s *Store) SetWireGuardReport(ctx context.Context, id int64, tokenHash string, rep *agentproto.WireGuardReport) error {
	if rep != nil && len(rep.Links) > 64 {
		return fmt.Errorf("too many WireGuard statuses")
	}
	if rep != nil && rep.Setup != nil {
		setup := rep.Setup
		if len(setup.Checks) > 32 || len(setup.Packages) > 16 || len(setup.Manager) > 32 {
			return fmt.Errorf("invalid WireGuard setup report")
		}
		for _, check := range setup.Checks {
			if len(check.ID) > 64 {
				return fmt.Errorf("invalid WireGuard check")
			}
		}
		for _, pkg := range setup.Packages {
			if len(pkg) > 64 {
				return fmt.Errorf("invalid WireGuard package")
			}
		}
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE nodes SET wireguard_json=?,wireguard_reported_at=? WHERE id=? AND token_hash=?`, string(raw), now(), id, tokenHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) WireGuardReport(ctx context.Context, id int64) (*agentproto.WireGuardReport, time.Time, error) {
	var raw string
	var stamp sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT wireguard_json,wireguard_reported_at FROM nodes WHERE id=?`, id).Scan(&raw, &stamp); err != nil {
		return nil, time.Time{}, wrapNotFound(err)
	}
	var rep *agentproto.WireGuardReport
	err := json.Unmarshal([]byte(raw), &rep)
	if !stamp.Valid {
		return rep, time.Time{}, err
	}
	return rep, unix(stamp.Int64), err
}

// A nonempty preference must never be silently ignored by an older transit.
// An absent field retains the routing used before preference support existed.
func (s *Store) WireGuardDomainPreferenceCapable(ctx context.Context, id int64) bool {
	rep, at, err := s.WireGuardReport(ctx, id)
	age := time.Since(at)
	return err == nil && rep != nil && rep.Available && rep.DomainPreferenceSupported && age >= 0 && age <= 3*time.Minute
}

func (s *Store) WireGuardForwardingCapable(ctx context.Context, id int64) bool {
	rep, at, err := s.WireGuardReport(ctx, id)
	age := time.Since(at)
	return err == nil && rep != nil && rep.Available && rep.ForwardingSupported && age >= 0 && age <= 3*time.Minute
}
func (s *Store) WireGuardCapable(ctx context.Context, id int64, transit bool) bool {
	n, err := s.NodeByID(ctx, id)
	if err != nil || !NodeProxyAllowed(n) || !n.Paired {
		return false
	}
	rep, at, err := s.WireGuardReport(ctx, id)
	age := time.Since(at)
	if err != nil || rep == nil || !rep.Available || age < 0 || age > 3*time.Minute {
		return false
	}
	if !transit {
		return true
	}
	st, err := s.NodeStatus(ctx, id)
	if err != nil {
		return false
	}
	var cores map[string]agentproto.CoreStatus
	if json.Unmarshal(st.Cores, &cores) != nil {
		return false
	}
	c := cores["xray"]
	return c.Capabilities != nil && c.Capabilities.Supports(spec.Inbound{Protocol: spec.VLESS, WireGuardSlot: 1})
}
