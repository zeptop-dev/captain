package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// Subscription assembles a user's subscription lines and account summary.
type Subscription struct {
	Store *store.Store
}

// ErrNoAccess means the user has no usable subscription (expired, out of
// quota, never bought one): the document is empty but the usage header
// still tells the client what happened.
var ErrNoAccess = errors.New("subscription: no usable subscription")

// ErrDisabled means the account itself is banned: the subscription URL is
// refused outright (403), nothing about the account leaks.
var ErrDisabled = errors.New("subscription: account disabled")

// Lines returns what the user may connect to, or ErrNoAccess.
func (s *Subscription) Lines(ctx context.Context, u *domain.User, at time.Time) ([]subscription.Line, subscription.Account, error) {
	subs, err := s.Store.ActiveSubscriptions(ctx, u.ID)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	if u.Status != "active" {
		return nil, subscription.Account{}, ErrDisabled
	}
	usable := usableSubs(subs, at)
	if len(usable) == 0 {
		return nil, account(subs), ErrNoAccess
	}
	rows, err := s.Store.EntriesForUser(ctx, u)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	var ss SubscriptionSettings
	_ = s.Store.GetSetting(ctx, SettingSubscription, &ss)
	acct := account(usable)
	vars := s.remarkVars(ctx, u, usable, acct, at)
	lines := make([]subscription.Line, 0, len(rows)+len(ss.InfoLines))
	for i, text := range ss.InfoLines {
		if text = strings.TrimSpace(text); text != "" {
			lines = append(lines, InfoLine(vars.Expand(text), i))
		}
	}
	for _, r := range rows {
		lines = append(lines, subscription.Line{
			Name: vars.Expand(subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags)), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
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
		l.Name = vars.Expand(subscription.WithFlag(n.Name, l.Host, "", ss.AutoFlags))
		lines = append(lines, l)
	}
	return lines, acct, nil
}

// remarkVars resolves the {{VARIABLE}} values for this user; plan names
// are looked up only when a name is needed.
func (s *Subscription) remarkVars(ctx context.Context, u *domain.User, usable []*domain.Subscription, acct subscription.Account, at time.Time) RemarkVars {
	plans := map[int64]string{}
	for _, sub := range usable {
		if _, seen := plans[sub.PlanID]; seen {
			continue
		}
		if p, err := s.Store.PlanByID(ctx, sub.PlanID); err == nil {
			plans[sub.PlanID] = p.Name
		}
	}
	return NewRemarkVars(u, usable, plans, acct, at)
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
	subs, _ := s.Store.ActiveSubscriptions(ctx, u.ID)
	usable := usableSubs(subs, time.Now())
	vars := s.remarkVars(ctx, u, usable, account(usable), time.Now())
	out := make([]EntryLink, 0, len(rows))
	for _, r := range rows {
		l := subscription.Line{
			Name: vars.Expand(subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags)), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
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

// HWIDLimit is the number of HWID devices user may register: the user's
// own override when set (0 = unlimited), else the largest device limit
// among their usable plans, else fallback (0 = unlimited).
func (s *Subscription) HWIDLimit(ctx context.Context, u *domain.User, fallback int) int {
	if u.HwidLimit != nil {
		return *u.HwidLimit
	}
	limits, err := s.Store.DeviceLimits(ctx)
	if err == nil {
		if n, ok := limits[u.ID]; ok && n > 0 {
			return n
		}
	}
	return fallback
}
