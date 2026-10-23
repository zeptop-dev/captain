package service

import (
	"context"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/bosun/pkg/wg"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type WireGuardDeviceEntry struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type WireGuardDeviceView struct {
	store.WireGuardDevice
	SubscriptionURL string `json:"subscription_url"`
}

type WireGuardDevicesView struct {
	Available     bool                   `json:"available"`
	Reason        string                 `json:"reason"`
	Limit         int                    `json:"limit"`
	LegacyEnabled bool                   `json:"legacy_enabled"`
	Devices       []WireGuardDeviceView  `json:"devices"`
	Entries       []WireGuardDeviceEntry `json:"entries"`
}

// eligibleWireGuardLines keeps all existing plan, group, block and profile checks.
// Device URLs are WG-only credentials: they never reveal the parent's ordinary proxies.
func (s *Subscription) eligibleWireGuardLines(ctx context.Context, u *domain.User, at time.Time, profile *store.SubscriptionProfile) ([]PublishedLine, subscription.Account, string, error) {
	lines, acct, err := s.PublishedLinesWithProfile(ctx, u, at, profile)
	if errors.Is(err, ErrDisabled) || errors.Is(err, ErrNoAccess) {
		return nil, acct, "no_wireguard", nil
	}
	if err != nil {
		return nil, acct, "unavailable", err
	}
	out := []PublishedLine{}
	reason := "no_wireguard"
	capable := map[int64]bool{}
	for _, line := range lines {
		if line.EntryID <= 0 || line.Line.Inbound.Protocol != spec.WireGuard {
			continue
		}
		reason = "upgrade_required"
		entry, err := s.Store.EntryByID(ctx, line.EntryID)
		if err != nil {
			return nil, acct, "unavailable", err
		}
		ib, err := s.Store.InboundByID(ctx, entry.InboundID)
		if err != nil {
			return nil, acct, "unavailable", err
		}
		ok, seen := capable[ib.NodeID]
		if !seen {
			ok = s.Store.WireGuardDevicesCapable(ctx, ib.NodeID)
			capable[ib.NodeID] = ok
		}
		// A WG-only credential cannot expose a non-WG dialer credential.
		if !ok || line.DialerEntryID != 0 {
			continue
		}
		out = append(out, line)
	}
	if len(out) > 0 {
		reason = ""
	}
	return out, acct, reason, nil
}

func (s *Subscription) WireGuardDevices(ctx context.Context, u *domain.User, urlFor func(string) string) (*WireGuardDevicesView, error) {
	profile, err := s.Store.SubscriptionProfileForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	lines, _, reason, err := s.eligibleWireGuardLines(ctx, u, time.Now(), profile)
	if err != nil {
		return nil, err
	}
	limit := s.HWIDLimitWithProfile(ctx, u, store.MaxWireGuardDevices, profile)
	if limit <= 0 || limit > store.MaxWireGuardDevices {
		limit = store.MaxWireGuardDevices
	}
	rows, err := s.Store.WireGuardDevices(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	v := &WireGuardDevicesView{Available: len(lines) > 0, Reason: reason, Limit: limit, LegacyEnabled: true, Devices: []WireGuardDeviceView{}, Entries: []WireGuardDeviceEntry{}}
	for _, d := range rows {
		url := ""
		if urlFor != nil {
			url = urlFor(d.Token)
		}
		v.Devices = append(v.Devices, WireGuardDeviceView{WireGuardDevice: d, SubscriptionURL: url})
	}
	for _, l := range lines {
		v.Entries = append(v.Entries, WireGuardDeviceEntry{ID: l.EntryID, Name: l.Line.Name})
	}
	return v, nil
}

func (s *Subscription) PublishedWireGuardDeviceLines(ctx context.Context, u *domain.User, d *store.WireGuardDevice, at time.Time, profile *store.SubscriptionProfile) ([]PublishedLine, subscription.Account, error) {
	if d == nil || d.UserID != u.ID {
		return nil, subscription.Account{}, store.ErrNotFound
	}
	lines, acct, _, err := s.eligibleWireGuardLines(ctx, u, at, profile)
	if err != nil {
		return nil, acct, err
	}
	addr, err := wg.DeviceAddress(d.ID)
	if err != nil {
		return nil, acct, err
	}
	for i := range lines {
		lines[i].Line.WireGuardPeer = &spec.WireGuardPeer{UUID: d.UUID, Address: addr}
	}
	return lines, acct, nil
}
