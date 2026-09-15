package service

import (
	"context"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// Subscription assembles a user's subscription lines and account summary.
type Subscription struct {
	Store *store.Store
}

// ErrNoAccess means the user has no usable subscription.
var ErrNoAccess = errors.New("subscription: no usable subscription")

// Lines returns what the user may connect to, or ErrNoAccess.
func (s *Subscription) Lines(ctx context.Context, u *domain.User, at time.Time) ([]subscription.Line, subscription.Account, error) {
	subs, err := s.Store.ActiveSubscriptions(ctx, u.ID)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	usable := usableSubs(subs, at)
	if len(usable) == 0 || u.Status != "active" {
		return nil, account(subs), ErrNoAccess
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
			Inbound: r.Inbound.Spec(), UUID: u.UUID, UserID: u.ID, Password: u.UUID, Tags: r.Entry.Tags, Extra: r.Entry.ClientExtra,
		})
	}
	// External nodes (imported share links) follow the panel's own entries.
	groups, err := s.Store.AccessGroups(ctx, u, at)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	ext, err := s.Store.ExternalNodesForGroup(ctx, groups)
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
	return lines, account(usable), nil
}

func usableSubs(subs []*domain.Subscription, at time.Time) []*domain.Subscription {
	var out []*domain.Subscription
	for _, sub := range subs {
		if sub.Usable(at) {
			out = append(out, sub)
		}
	}
	return out
}

// EntryLink is one entry as one user would receive it, with the share URI
// and whether the admin hid it from this user.
type EntryLink struct {
	EntryID  int64  `json:"entry_id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
	Blocked  bool   `json:"blocked"`
}

// EntryLinks lists every entry the user's group allows, blacklist included
// and flagged, so an admin can copy one user's link for one server or hide
// a server from that user. Access checks are the caller's business.
func (s *Subscription) EntryLinks(ctx context.Context, u *domain.User) ([]EntryLink, error) {
	rows, err := s.Store.EntriesForUserAll(ctx, u)
	if err != nil {
		return nil, err
	}
	blocked, err := s.Store.UserEntryBlocks(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	hidden := map[int64]bool{}
	for _, id := range blocked {
		hidden[id] = true
	}
	var ss SubscriptionSettings
	_ = s.Store.GetSetting(ctx, SettingSubscription, &ss)
	out := make([]EntryLink, 0, len(rows))
	for _, r := range rows {
		l := subscription.Line{
			Name: subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
			Inbound: r.Inbound.Spec(), UUID: u.UUID, UserID: u.ID, Password: u.UUID, Tags: r.Entry.Tags,
		}
		out = append(out, EntryLink{EntryID: r.Entry.ID, Name: l.Name, Protocol: string(r.Inbound.Protocol), Host: l.Host, Port: l.Port, URI: subscription.ShareURI(l), Blocked: hidden[r.Entry.ID]})
	}
	return out, nil
}

// account folds several subscriptions into the one summary the
// Subscription-Userinfo header can carry: usage and quota add up (any
// unlimited plan makes the total unlimited), expiry is the latest (never
// when any plan never expires).
func account(subs []*domain.Subscription) subscription.Account {
	var a subscription.Account
	unlimited, never := false, false
	for _, sub := range subs {
		a.Upload += sub.UsedUpBytes
		a.Download += sub.UsedDownBytes
		if sub.QuotaBytes == 0 {
			unlimited = true
		}
		a.Total += sub.QuotaBytes
		if sub.ExpiresAt == nil {
			never = true
		} else if e := sub.ExpiresAt.Unix(); e > a.Expire {
			a.Expire = e
		}
	}
	if unlimited {
		a.Total = 0
	}
	if never {
		a.Expire = 0
	}
	return a
}
