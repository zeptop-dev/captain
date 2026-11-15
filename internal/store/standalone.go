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

var ErrStandaloneAccess = errors.New("standalone inbound cannot be assigned to plans or subscription nodes")
var ErrStandaloneKeyRotation = errors.New("generate a new SS2022 key when switching from subscriber to standalone use")
var ErrStandaloneReferences = errors.New("remove subscription nodes, lines and existing access grants before switching to standalone use")

// All save paths, including topology changesets, share this transaction guard.
func guardStandaloneInboundTx(ctx context.Context, tx *sql.Tx, ib *domain.Inbound, create bool) error {
	if ib.Settings.Standalone == nil {
		return nil
	}
	if err := ib.Spec().Validate(); err != nil {
		return err
	}
	if ib.AccessPolicy != "" && ib.AccessPolicy != domain.AccessNone {
		return ErrStandaloneAccess
	}
	if create {
		if ib.GroupID != nil {
			return ErrStandaloneAccess
		}
		return nil
	}
	var old string
	var oldGroup sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT settings_json,group_id FROM inbounds WHERE id=?`, ib.ID).Scan(&old, &oldGroup); err != nil {
		return wrapNotFound(err)
	}
	if ib.GroupID != nil && (!oldGroup.Valid || *ib.GroupID != oldGroup.Int64) {
		return ErrStandaloneAccess
	}
	var previous spec.Inbound
	if err := json.Unmarshal([]byte(old), &previous); err != nil {
		return err
	}
	var refs int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM entries WHERE inbound_id=?) + (SELECT count(*) FROM forward_lines WHERE target_inbound_id=?)`, ib.ID, ib.ID).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrStandaloneReferences
	}
	if previous.Standalone == nil {
		// Subscriber configurations publish the shared server key. Making it
		// sufficient on its own would otherwise authorize former subscribers.
		if ib.Protocol == spec.Shadowsocks && previous.ServerKey != "" && previous.ServerKey == ib.Settings.ServerKey {
			return ErrStandaloneKeyRotation
		}
		var policy string
		if err := tx.QueryRowContext(ctx, `SELECT policy FROM effective_inbound_access WHERE inbound_id=?`, ib.ID).Scan(&policy); err != nil {
			return err
		}
		if policy != domain.AccessNone {
			return ErrStandaloneReferences
		}
	}
	// An unchanged legacy column may remain after explicit revocation. Clear it
	// only after proving there is no effective grant or dependent publication.
	ib.GroupID = nil
	return nil
}

func guardStandaloneEntryTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT settings_json FROM inbounds WHERE id=?`, id).Scan(&raw); err != nil {
		return wrapNotFound(err)
	}
	var ib spec.Inbound
	if err := json.Unmarshal([]byte(raw), &ib); err != nil {
		return err
	}
	if ib.Standalone != nil {
		return ErrStandaloneAccess
	}
	return nil
}

func (s *Store) StandaloneCapable(ctx context.Context, n *domain.Node, ib spec.Inbound, at time.Time) bool {
	if ib.Standalone == nil {
		return true
	}
	if n.LastSeenAt == nil || at.Sub(*n.LastSeenAt) >= 3*time.Minute || at.Sub(*n.LastSeenAt) < -30*time.Second {
		return false
	}
	st, err := s.NodeStatus(ctx, n.ID)
	if err != nil {
		return false
	}
	var cores map[string]agentproto.CoreStatus
	if json.Unmarshal(st.Cores, &cores) != nil {
		return false
	}
	candidates := agentproto.CoreCandidates(cores)
	if len(candidates) == 0 {
		return false
	}
	_, err = spec.SelectCore(ib, candidates)
	return err == nil
}
