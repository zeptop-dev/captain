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
	Remaining     int                    `json:"remaining"`
	SharedLimit   *int                   `json:"shared_limit,omitempty"`
	SharedUsed    *int                   `json:"shared_used,omitempty"`
	LegacyEnabled bool                   `json:"legacy_enabled"`
	Devices       []WireGuardDeviceView  `json:"devices"`
	Entries       []WireGuardDeviceEntry `json:"entries"`
}

// eligibleWireGuardLines keeps all existing plan, group, block and profile checks.
// Device URLs are WG-only credentials: they never reveal the parent's ordinary proxies.
func (s *Subscription) eligibleWireGuardLines(ctx context.Context, u *domain.User, at time.Time, profile *store.SubscriptionProfile, fresh bool, entryID int64) ([]PublishedLine, subscription.Account, string, error) {
	lines, acct, err := s.PublishedLinesWithProfile(ctx, u, at, profile)
	if errors.Is(err, ErrDisabled) || errors.Is(err, ErrNoAccess) {
		return nil, acct, "no_wireguard", nil
	}
	if err != nil {
		return nil, acct, "unavailable", err
	}
	out := []PublishedLine{}
	reason := "no_wireguard"
	unavailable := false
	capable := map[int64]string{}
	for _, line := range lines {
		if line.EntryID <= 0 || line.Line.Inbound.Protocol != spec.WireGuard || (entryID > 0 && line.EntryID != entryID) {
			continue
		}
		entry, err := s.Store.EntryByID(ctx, line.EntryID)
		if err != nil {
			return nil, acct, "unavailable", err
		}
		ib, err := s.Store.InboundByID(ctx, entry.InboundID)
		if err != nil {
			return nil, acct, "unavailable", err
		}
		why, seen := capable[ib.NodeID]
		if !seen {
			why = s.Store.WireGuardDevicesCapability(ctx, ib.NodeID, fresh)
			capable[ib.NodeID] = why
		}
		// A WG-only credential cannot expose a non-WG dialer credential.
		if line.DialerEntryID != 0 {
			if reason == "no_wireguard" {
				reason = "client_chain_unsupported"
			}
			continue
		}
		if why != "" {
			reason = why
			unavailable = true
			continue
		}
		// Device links have a deliberately narrow credential scope. Arbitrary
		// client overlays could replace the protocol, key or dialer, so do not
		// inherit them into an independently revocable WG credential.
		line.Extra = nil
		out = append(out, line)
	}
	// Do not let a transiently unconfirmed server silently remove only part
	// of a previously installed device subscription either.
	if !fresh && unavailable {
		return nil, acct, reason, ErrWireGuardDevicesUnavailable
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
	lines, _, reason, err := s.eligibleWireGuardLines(ctx, u, time.Now(), profile, true, 0)
	if err != nil {
		return nil, err
	}
	limit, shared := s.wireGuardDeviceLimits(ctx, u, profile)
	rows, err := s.Store.WireGuardDevices(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	v := &WireGuardDevicesView{Available: len(lines) > 0, Reason: reason, Limit: limit, LegacyEnabled: true, Devices: []WireGuardDeviceView{}, Entries: []WireGuardDeviceEntry{}}
	v.Remaining = max(0, limit-len(rows))
	if shared != nil {
		hwids, err := s.Store.HwidDevices(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		used := len(hwids) + len(rows)
		v.SharedLimit, v.SharedUsed = shared, &used
		if *shared > 0 {
			v.Remaining = min(v.Remaining, max(0, *shared-used))
		}
	}
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

func (s *Subscription) PublishedWireGuardDeviceLines(ctx context.Context, u *domain.User, d *store.WireGuardDevice, at time.Time, profile *store.SubscriptionProfile, selectedEntry ...int64) ([]PublishedLine, subscription.Account, error) {
	if u.Role != domain.RoleUser || u.Status != "active" {
		return nil, subscription.Account{}, ErrDisabled
	}
	if d == nil || d.UserID != u.ID {
		return nil, subscription.Account{}, store.ErrNotFound
	}
	entryID := int64(0)
	if len(selectedEntry) > 0 {
		entryID = selectedEntry[0]
	}
	lines, acct, reason, err := s.eligibleWireGuardLines(ctx, u, at, profile, false, entryID)
	if err != nil {
		return nil, acct, err
	}
	if len(lines) == 0 {
		if reason == "no_wireguard" || reason == "client_chain_unsupported" {
			return nil, acct, store.ErrNotFound
		}
		return nil, acct, ErrWireGuardDevicesUnavailable
	}
	addr, err := wg.DeviceAddress(d.AddressSlot)
	if err != nil {
		return nil, acct, err
	}
	for i := range lines {
		lines[i].Line.WireGuardPeer = &spec.WireGuardPeer{UUID: d.UUID, Address: addr}
		if _, ok := subscription.WGClient(lines[i].Line); !ok {
			return nil, acct, ErrWireGuardDevicesUnavailable
		}
	}
	return lines, acct, nil
}

// Unconfirmed or incompatible binary state must not publish an empty subscription,
// which clients commonly interpret as deleting all previously installed devices.
var ErrWireGuardDevicesUnavailable = errors.New("WireGuard device service unavailable")

func (s *Subscription) wireGuardDeviceLimits(ctx context.Context, u *domain.User, profile *store.SubscriptionProfile) (int, *int) {
	var settings SubscriptionSettings
	_ = s.Store.GetSetting(ctx, SettingSubscription, &settings)
	ApplySubscriptionProfile(&settings, profile)
	fallback := store.MaxWireGuardDevices
	if settings.HWID.Enabled {
		fallback = settings.HWID.FallbackLimit
	}
	shared := s.HWIDLimitWithProfile(ctx, u, fallback, profile)
	native := shared
	if native <= 0 || native > store.MaxWireGuardDevices {
		native = store.MaxWireGuardDevices
	}
	if settings.HWID.Enabled {
		return native, &shared
	}
	return native, nil
}

func (s *Subscription) CreateWireGuardDevice(ctx context.Context, u *domain.User, name string) (*store.WireGuardDevice, error) {
	profile, err := s.Store.SubscriptionProfileForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	limit, shared := s.wireGuardDeviceLimits(ctx, u, profile)
	return s.Store.CreateWireGuardDeviceWithHWID(ctx, u.ID, name, limit, shared)
}
