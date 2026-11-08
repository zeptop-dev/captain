package store

import (
	"context"
	"errors"
	"fmt"

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
