package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrReverseConflict = errors.New("reverse connections changed; reload before saving")

type ReverseLink struct {
	ID                string `json:"id"`
	ExitID            int64  `json:"exit_id"`
	TransitID         int64  `json:"transit_id"`
	UserInboundID     int64  `json:"user_inbound_id"`
	ReceiverInboundID int64  `json:"receiver_inbound_id"`
	EntryID           int64  `json:"entry_id"`
	Version           int64  `json:"version"`
}

func (s *Store) ReverseLinks(ctx context.Context, nodeID int64) ([]ReverseLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, exit_id, transit_id, user_inbound_id, receiver_inbound_id, entry_id, version FROM node_reverse_links WHERE exit_id = ? OR transit_id = ? ORDER BY id`, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReverseLink{}
	for rows.Next() {
		var l ReverseLink
		if err := rows.Scan(&l.ID, &l.ExitID, &l.TransitID, &l.UserInboundID, &l.ReceiverInboundID, &l.EntryID, &l.Version); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) ReverseCapable(ctx context.Context, nodeID int64) bool {
	st, err := s.NodeStatus(ctx, nodeID)
	if err != nil {
		return false
	}
	var cs map[string]agentproto.CoreStatus
	if json.Unmarshal(st.Cores, &cs) != nil {
		return false
	}
	c := cs["xray"]
	return c.Capabilities != nil && c.Capabilities.VLESSReverse
}

func (s *Store) ReverseOwnedInbound(ctx context.Context, id int64) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM node_reverse_links WHERE user_inbound_id = ? OR receiver_inbound_id = ?) + (SELECT COUNT(*) FROM node_wireguard_links WHERE user_inbound_id = ?)`, id, id, id).Scan(&n)
	return err != nil || n > 0
}
func (s *Store) ReverseOwnedEntry(ctx context.Context, id int64) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM node_reverse_links WHERE entry_id = ?) + (SELECT COUNT(*) FROM node_wireguard_links WHERE entry_id = ?)`, id, id).Scan(&n)
	return err != nil || n > 0
}

// EntryConnection is presentation-only ownership metadata for the unified
// catalogue. Managed entries must be edited through their complete connection.
type EntryConnection struct {
	Type       string `json:"type"`
	ExitNodeID int64  `json:"exit_node_id"`
}

func (s *Store) EntryConnections(ctx context.Context) (map[int64]*EntryConnection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id,exit_id,'reverse' FROM node_reverse_links UNION ALL SELECT entry_id,exit_id,'wireguard' FROM node_wireguard_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*EntryConnection{}
	for rows.Next() {
		var id int64
		v := &EntryConnection{}
		if err := rows.Scan(&id, &v.ExitNodeID, &v.Type); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

type ReverseWrite struct {
	Link           ReverseLink
	User, Receiver domain.Inbound
	Entry          domain.Entry
}

// SaveReverseLinks replaces one exit's set atomically. Callers hold Topology
// through validation. Expected IDs/versions also reject concurrent editors.
func (s *Store) SaveReverseLinks(ctx context.Context, exitID int64, expected map[string]int64, writes []ReverseWrite) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,version FROM node_reverse_links WHERE exit_id = ?`, exitID)
	if err != nil {
		return err
	}
	current := map[string]int64{}
	for rows.Next() {
		var id string
		var v int64
		if err = rows.Scan(&id, &v); err != nil {
			rows.Close()
			return err
		}
		current[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(current) != len(expected) {
		return ErrReverseConflict
	}
	for id, v := range current {
		if expected[id] != v {
			return ErrReverseConflict
		}
	}
	keep := map[string]bool{}
	for i := range writes {
		w := &writes[i]
		keep[w.Link.ID] = true
		for _, ib := range []*domain.Inbound{&w.User, &w.Receiver} {
			if err = saveReverseInbound(ctx, tx, ib); err != nil {
				return err
			}
		}
		e := &w.Entry
		e.InboundID = w.User.ID
		if e.ID == 0 {
			res, e2 := tx.ExecContext(ctx, `INSERT INTO entries(name,inbound_id,display_host,display_port,rate,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, e.Name, e.InboundID, e.DisplayHost, e.DisplayPort, 1, boolInt(e.Enabled), now(), now())
			if e2 != nil {
				return e2
			}
			e.ID, _ = res.LastInsertId()
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE entries SET name=?,display_host=?,display_port=?,enabled=?,updated_at=? WHERE id=?`, e.Name, e.DisplayHost, e.DisplayPort, boolInt(e.Enabled), now(), e.ID)
			if err != nil {
				return err
			}
		}
		if w.Link.Version == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO node_reverse_links(id,exit_id,transit_id,user_inbound_id,receiver_inbound_id,entry_id) VALUES(?,?,?,?,?,?)`, w.Link.ID, exitID, w.Link.TransitID, w.User.ID, w.Receiver.ID, e.ID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE node_reverse_links SET version=version+1 WHERE id=?`, w.Link.ID)
		}
		if err != nil {
			return err
		}
	}
	for id := range current {
		if !keep[id] {
			var userID, receiverID int64
			if err = tx.QueryRowContext(ctx, `SELECT user_inbound_id,receiver_inbound_id FROM node_reverse_links WHERE id=?`, id).Scan(&userID, &receiverID); err != nil {
				return err
			}
			for _, inboundID := range []int64{userID, receiverID} {
				if err = checkReverseInboundRemoval(ctx, tx, inboundID); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM node_reverse_links WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func checkReverseInboundRemoval(ctx context.Context, tx *sql.Tx, id int64) error {
	if used, err := rolloutInboundReferencedTx(ctx, tx, id); err != nil {
		return err
	} else if used {
		return ErrLineInUse
	}
	if err := checkClientChainRemoval(ctx, tx, "inbound", id); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM forward_lines WHERE target_inbound_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrLineInUse
	}
	if used, err := hasInternalForwardReference(ctx, tx, 0, id); err != nil {
		return err
	} else if used {
		return ErrLineInUse
	}
	return nil
}

func saveReverseInbound(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	if err := checkRolloutInboundTx(ctx, tx, ib); err != nil {
		return err
	}
	b, err := json.Marshal(ib.Settings)
	if err != nil {
		return err
	}
	args := []any{ib.NodeID, ib.Tag, ib.Protocol, ib.Listen, ib.Port, ib.Core, string(b), nullInt64(ib.GroupID), boolInt(ib.Enabled), nullInt64(ib.IngressID), now()}
	if ib.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO inbounds(node_id,tag,protocol,listen,port,core,settings_json,group_id,enabled,ingress_id,updated_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, append(args, now())...)
		if err != nil {
			return err
		}
		ib.ID, _ = res.LastInsertId()
		return nil
	}
	if err := guardLegacyAccessTx(ctx, tx, ib); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE inbounds SET node_id=?,tag=?,protocol=?,listen=?,port=?,core=?,settings_json=?,group_id=?,enabled=?,ingress_id=?,updated_at=? WHERE id=?`, append(args, ib.ID)...)
	if err != nil {
		return err
	}
	return refreshLinesForNodeTx(ctx, tx, ib.NodeID)
}

// PrivateAccessCapable is also used at state delivery: a downgraded node must
// not receive an enabled policy it would silently ignore.
func (s *Store) PrivateAccessCapable(ctx context.Context, nodeID int64, ib spec.Inbound) bool {
	if !ib.PrivateAccess.Enabled() {
		return true
	}
	st, err := s.NodeStatus(ctx, nodeID)
	if err != nil {
		return false
	}
	var cs map[string]agentproto.CoreStatus
	if json.Unmarshal(st.Cores, &cs) != nil {
		return false
	}
	candidates := agentproto.CoreCandidates(cs)
	for i := range candidates {
		c := &candidates[i].Capabilities
		if r := c.PrivateAccessReadiness; r != nil {
			age := time.Now().Unix() - r.CheckedAt
			if r.CheckedAt <= 0 || age >= 180 || age < -30 {
				c.PrivateAccess = false
				c.CoreOnlyPrivateAccess = false
			}
		}
	}
	if candidates == nil {
		return false
	}
	_, err = spec.SelectCore(ib, candidates)
	if err != nil {
		return false
	}
	routing, err := s.NodeRouting(ctx, nodeID)
	if err != nil {
		return false
	}
	policy := &spec.Node{Inbounds: []spec.Inbound{ib}, Outbounds: routing.Outbounds, Routes: routing.Routes}
	return policy.ValidatePrivateAccessRouting(candidates) == nil
}
