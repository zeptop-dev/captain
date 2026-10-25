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

func (s *Subscription) PublishedLines(ctx context.Context, u *domain.User, at time.Time) ([]PublishedLine, subscription.Account, error) {
	profile, err := s.Store.SubscriptionProfileForUser(ctx, u.ID)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	return s.PublishedLinesWithProfile(ctx, u, at, profile)
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
	profile, err := s.Store.SubscriptionProfileForUser(ctx, u.ID)
	if err != nil {
		return nil, subscription.Account{}, err
	}
	return s.LinesWithProfile(ctx, u, at, profile)
}

// LinesWithProfile previews or serves one explicit presentation policy. Access
// groups, quota checks and node credentials remain determined by the customer.
func (s *Subscription) LinesWithProfile(ctx context.Context, u *domain.User, at time.Time, profile *store.SubscriptionProfile) ([]subscription.Line, subscription.Account, error) {
	lines, acct, err := s.PublishedLinesWithProfile(ctx, u, at, profile)
	if err != nil {
		return nil, acct, err
	}
	// Legacy callers request standalone connection profiles. A dependent node
	// must not be exported as a direct connection by an unaware caller.
	out, err := ClientLines(lines, "uri")
	return out, acct, err
}

// PublishedLinesWithProfile keeps client-chain identity separate from optional
// client fields. Only ClientLines translates those references into client names.
func (s *Subscription) PublishedLinesWithProfile(ctx context.Context, u *domain.User, at time.Time, profile *store.SubscriptionProfile) ([]PublishedLine, subscription.Account, error) {
	if u.Role != domain.RoleUser || u.Status != "active" {
		return nil, subscription.Account{}, ErrDisabled
	}
	subs, err := s.Store.ActiveSubscriptions(ctx, u.ID)
	if err != nil {
		return nil, subscription.Account{}, err
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
	ApplySubscriptionProfile(&ss, profile)
	prefix := ""
	if profile != nil {
		prefix = profile.Settings.RemarkPrefix
	}
	acct := account(usable)
	vars := s.remarkVars(ctx, u, usable, acct, at)
	lines := make([]PublishedLine, 0, len(rows)+len(ss.InfoLines))
	for i, text := range ss.InfoLines {
		if text = strings.TrimSpace(text); text != "" {
			lines = append(lines, PublishedLine{Line: InfoLine(vars.Expand(text), i)})
		}
	}
	for _, r := range rows {
		line := PublishedLine{EntryID: r.Entry.ID, Line: subscription.Line{
			Name: vars.Expand(prefix + subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags)), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
			Region: r.Entry.Region, RegionName: r.Entry.Name,
			Inbound: r.Inbound.Spec(), UUID: u.UUID, UserID: u.AgentID, Password: u.UUID, Tags: r.Entry.Tags, Extra: r.Entry.ClientExtra,
		}}
		if r.Entry.ClientDialerEntryID != nil {
			line.DialerEntryID = *r.Entry.ClientDialerEntryID
		}
		if r.Entry.CategoryID != nil {
			line.CategoryID = *r.Entry.CategoryID
		}
		lines = append(lines, line)
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
		l.Name = vars.Expand(prefix + subscription.WithFlag(n.Name, l.Host, "", ss.AutoFlags))
		// Group by the undecorated label, not a flag inferred from an ingress
		// hostname or a profile prefix that happens to contain a region name.
		l.RegionName = n.Name
		lines = append(lines, PublishedLine{Line: l})
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
	EntryID     int64  `json:"entry_id"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	URI         string `json:"uri"`
	Blocked     bool   `json:"blocked"`
	ClientChain bool   `json:"client_chain,omitempty"`
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
	profile, err := s.Store.SubscriptionProfileForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	ApplySubscriptionProfile(&ss, profile)
	prefix := ""
	if profile != nil {
		prefix = profile.Settings.RemarkPrefix
	}
	subs, _ := s.Store.ActiveSubscriptions(ctx, u.ID)
	usable := usableSubs(subs, time.Now())
	vars := s.remarkVars(ctx, u, usable, account(usable), time.Now())
	out := make([]EntryLink, 0, len(rows))
	for _, r := range rows {
		l := subscription.Line{
			Name: vars.Expand(prefix + subscription.WithFlag(r.Entry.Name, r.Entry.DisplayHost, r.Entry.Region, ss.AutoFlags)), Host: r.Entry.DisplayHost, Port: r.Entry.DisplayPort,
			Inbound: r.Inbound.Spec(), UUID: u.UUID, UserID: u.AgentID, Password: u.UUID, Tags: r.Entry.Tags,
		}
		uri := subscription.ShareURI(l)
		if r.Entry.ClientDialerEntryID != nil {
			uri = ""
		}
		out = append(out, EntryLink{EntryID: r.Entry.ID, Name: l.Name, Protocol: string(r.Inbound.Protocol), Host: l.Host, Port: l.Port, URI: uri, Blocked: hidden[r.Entry.ID], ClientChain: r.Entry.ClientDialerEntryID != nil})
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

// ApplySubscriptionProfile overlays only presentation/device settings. It never
// grants an entry, plan, balance or proxy credential.
func ApplySubscriptionProfile(ss *SubscriptionSettings, p *store.SubscriptionProfile) {
	if p == nil {
		return
	}
	v := p.Settings
	if v.AutoFlags != nil {
		ss.AutoFlags = *v.AutoFlags
	}
	if v.InfoLines != nil {
		ss.InfoLines = *v.InfoLines
	}
	if v.HWID != nil {
		h := v.HWID
		if h.Enabled != nil {
			ss.HWID.Enabled = *h.Enabled
			if !*h.Enabled {
				ss.HWID.Require = false
			}
		}
		if h.Require != nil {
			ss.HWID.Require = *h.Require
			if *h.Require {
				ss.HWID.Enabled = true
			}
		}
		if h.Announce != nil {
			ss.HWID.Announce = *h.Announce
		}
	}
}
func (s *Subscription) HWIDLimitWithProfile(ctx context.Context, u *domain.User, fallback int, p *store.SubscriptionProfile) int {
	if u.HwidLimit != nil {
		return *u.HwidLimit
	}
	if p != nil && p.Settings.HWID != nil && p.Settings.HWID.DeviceLimit != nil {
		return *p.Settings.HWID.DeviceLimit
	}
	return s.HWIDLimit(ctx, u, fallback)
}
