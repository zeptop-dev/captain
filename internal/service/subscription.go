package service

import (
	"context"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subscription"
)

// Subscription assembles a user's subscription lines and account summary.
type Subscription struct {
	Store *store.Store
}

// ErrNoAccess means the user has no usable subscription.
var ErrNoAccess = errors.New("subscription: no usable subscription")

// Lines returns what the user may connect to, or ErrNoAccess.
func (s *Subscription) Lines(ctx context.Context, u *domain.User, at time.Time) ([]subscription.Line, subscription.Account, error) {
	sub, err := s.Store.ActiveSubscription(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !sub.Usable(at)) || u.Status != "active" {
		acct := subscription.Account{}
		if sub != nil {
			acct = account(sub)
		}
		return nil, acct, ErrNoAccess
	}
	if err != nil {
		return nil, subscription.Account{}, err
	}
	rows, err := s.Store.EntriesForUser(ctx, u)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	var ss SubscriptionSettings
	_ = s.Store.GetSetting(ctx, SettingSubscription, &ss)
	lines := make([]subscription.Line, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, subscription.Line{
			Name: subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
			Inbound: r.Inbound.Spec(), UUID: u.UUID, Password: u.UUID, Tags: r.Entry.Tags,
		})
	}
	// External nodes (imported share links) follow the panel's own entries.
	ext, err := s.Store.ExternalNodesForGroup(ctx, u.GroupID)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	for _, n := range ext {
		l, err := subscription.ParseURI(n.URI)
		if err != nil {
			continue
		}
		l.Name = subscription.WithFlag(n.Name, l.Host, "", ss.AutoFlags)
		lines = append(lines, l)
	}
	return lines, account(sub), nil
}

func account(sub *domain.Subscription) subscription.Account {
	a := subscription.Account{Upload: sub.UsedUpBytes, Download: sub.UsedDownBytes, Total: sub.QuotaBytes}
	if sub.ExpiresAt != nil {
		a.Expire = sub.ExpiresAt.Unix()
	}
	return a
}
