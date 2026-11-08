package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrEntryIPVersion = errors.New("invalid entry IP version")

func validateEntryIPVersion(e *domain.Entry) error {
	if e.IPVersion != "" && e.ClientDialerEntryID != nil {
		return fmt.Errorf("%w: client-chain landing addresses are resolved by the front proxy; configure its directly connected entry instead", ErrEntryIPVersion)
	}
	if err := subscription.ValidateIPVersion(e.DisplayHost, e.IPVersion, e.ClientExtra); err != nil {
		return fmt.Errorf("%w: %v", ErrEntryIPVersion, err)
	}
	return nil
}

// The protocol must be the final resolved listener's protocol. WireGuard's
// current client renderers cannot pin only its endpoint resolver, so a strict
// hostname policy would otherwise create a node omitted from every format.
func validateEntryIPVersionForProtocol(e *domain.Entry, protocol spec.Protocol) error {
	if err := validateEntryIPVersion(e); err != nil {
		return err
	}
	if protocol == spec.WireGuard && (e.IPVersion == "ipv4" || e.IPVersion == "ipv6") && subscription.IPVersionSupport("clash", subscription.Line{Host: e.DisplayHost, IPVersion: e.IPVersion, Inbound: spec.Inbound{Protocol: protocol}}) != "" {
		return fmt.Errorf("%w: WireGuard requires a literal IP address for an IPv4-only or IPv6-only policy; use dual stack for a hostname", ErrEntryIPVersion)
	}
	return nil
}

func validateEntryIPVersionTx(ctx context.Context, tx *sql.Tx, e *domain.Entry) error {
	if err := validateEntryIPVersion(e); err != nil {
		return err
	}
	if e.IPVersion == "" || e.IPVersion == "dual" {
		return nil
	}
	var protocol spec.Protocol
	if err := tx.QueryRowContext(ctx, `SELECT protocol FROM inbounds WHERE id=?`, e.InboundID).Scan(&protocol); err != nil {
		return wrapNotFound(err)
	}
	return validateEntryIPVersionForProtocol(e, protocol)
}

func validateInboundEntryIPVersionsTx(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+entryCols+` FROM entries e WHERE e.inbound_id=?`, ib.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return err
		}
		if err = validateEntryIPVersionForProtocol(e, ib.Protocol); err != nil {
			return err
		}
	}
	return rows.Err()
}

// CreateEntryVariants publishes three client choices with one shared listener.
// Access is changed once, accounting for all three new references. Any insertion
// failure rolls back both the entries and the access edit.
func (s *Store) CreateEntryVariants(ctx context.Context, base *domain.Entry) ([]*domain.Entry, error) {
	next := *base
	next.ID = 0
	if err := s.ResolveLineEntry(ctx, &next); err != nil {
		return nil, err
	}
	if err := s.ValidateClientChain(ctx, &next); err != nil {
		return nil, err
	}
	if next.Rate == 0 {
		next.Rate = 1
	}
	entries := make([]*domain.Entry, 0, 3)
	for _, mode := range []struct{ value, label string }{{"ipv4", "IPv4"}, {"ipv6", "IPv6"}, {"dual", "Dual"}} {
		e := next
		e.ID, e.Name, e.IPVersion, e.Access = 0, base.Name+" · "+mode.label, mode.value, nil
		if err := validateEntryIPVersion(&e); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if next.Access != nil {
		if err = updateInboundAccessTx(ctx, tx, next.InboundID, *next.Access, 3); err != nil {
			return nil, err
		}
	}
	for _, e := range entries {
		if err = writeEntryTx(ctx, tx, e, true); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}
