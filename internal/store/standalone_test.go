package store

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestStandaloneAccessAndTransitionGuards(t *testing.T) {
	f := newAccessFixture(t)
	ib := &domain.Inbound{NodeID: f.n.ID, Tag: "independent", Protocol: spec.SOCKS, Port: 1080, Enabled: true, Settings: spec.Inbound{Standalone: &spec.StandaloneAuth{Username: "operator", Password: "secret"}}}
	if err := f.s.CreateInbound(f.ctx, ib); err != nil {
		t.Fatal(err)
	}
	a, err := f.s.InboundAccess(f.ctx, ib.ID)
	if err != nil || a.Policy != domain.AccessNone {
		t.Fatal("standalone inherited legacy access", a, err)
	}
	members, err := f.s.UsersWithInboundAccess(f.ctx, ib.ID, time.Now())
	if err != nil || len(members) != 0 {
		t.Fatal("standalone has customer accounts", err)
	}
	change := f.change(t, ib.ID, domain.AccessAll)
	if err := f.s.UpdateInboundAccess(f.ctx, ib.ID, change); err == nil {
		t.Fatal("legacy access API assigned standalone listener")
	}
	catalog, err := f.s.AccessCatalog(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateGroupAccess(f.ctx, f.groups[0], GroupAccessChange{InboundIDs: []int64{ib.ID}, Revision: catalog.Revision, ConfirmShared: true}); err == nil {
		t.Fatal("plan/group API assigned standalone listener")
	}
	if _, err := LineProtocol(ib); !errors.Is(err, ErrStandaloneAccess) {
		t.Fatal("standalone offered as managed landing", err)
	}
	entry := &domain.Entry{Name: "Forbidden", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 1080, Enabled: true}
	if err := f.s.CreateEntry(f.ctx, entry); !errors.Is(err, ErrStandaloneAccess) {
		t.Fatal("standalone published", err)
	}
	for _, row := range catalog.Inbounds {
		if row.InboundID == ib.ID && row.Editable {
			t.Fatal("standalone offered to plans")
		}
	}
	// Existing subscriber listeners must retain references and credentials on rejection.
	old := *f.inbounds[0]
	old.Protocol = spec.SOCKS
	old.GroupID = nil
	old.Settings = ib.Settings
	if err := f.s.UpdateInbound(f.ctx, &old); !errors.Is(err, ErrStandaloneReferences) {
		t.Fatal("used listener converted", err)
	}
	stored, err := f.s.InboundByID(f.ctx, old.ID)
	if err != nil || stored.Protocol != spec.VLESS || stored.Settings.Standalone != nil {
		t.Fatal("failed conversion changed saved listener", err)
	}
	// Switching back cannot resurrect the all-users fallback or former grants.
	ib.Settings.Standalone = nil
	if err := f.s.UpdateInbound(f.ctx, ib); err != nil {
		t.Fatal(err)
	}
	a, err = f.s.InboundAccess(f.ctx, ib.ID)
	if err != nil || a.Policy != domain.AccessNone {
		t.Fatal("switch restored access", a, err)
	}
}

func TestStandaloneSS2022ConversionRotatesPublishedServerKey(t *testing.T) {
	f := newAccessFixture(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 16))
	ib := &domain.Inbound{NodeID: f.n.ID, Tag: "old-ss", Protocol: spec.Shadowsocks, Port: 8388, Enabled: true, AccessPolicy: domain.AccessNone, Settings: spec.Inbound{Cipher: "2022-blake3-aes-128-gcm", ServerKey: key}}
	if err := f.s.CreateInbound(f.ctx, ib); err != nil {
		t.Fatal(err)
	}
	ib.Settings.Standalone = &spec.StandaloneAuth{}
	if err := f.s.UpdateInbound(f.ctx, ib); !errors.Is(err, ErrStandaloneKeyRotation) {
		t.Fatal("formerly published server key became sufficient for standalone access", err)
	}
	saved, err := f.s.InboundByID(f.ctx, ib.ID)
	if err != nil || saved.Settings.Standalone != nil || saved.Settings.ServerKey != key {
		t.Fatal("failed conversion changed key or mode", err)
	}
	ib.Settings.ServerKey = ""
	ib.Settings = ib.Spec()
	spec.FillInboundSecrets(&ib.Settings)
	if err := f.s.UpdateInbound(f.ctx, ib); err != nil {
		t.Fatal(err)
	}
	saved, err = f.s.InboundByID(f.ctx, ib.ID)
	if err != nil || saved.Settings.Standalone == nil || saved.Settings.ServerKey == key {
		t.Fatal("new independent key not saved", err)
	}
}

func TestStandaloneConversionClearsRevokedLegacyGroupOnly(t *testing.T) {
	f := newAccessFixture(t)
	group := f.groups[0]
	ib := &domain.Inbound{NodeID: f.n.ID, Tag: "legacy-socks", Protocol: spec.SOCKS, Port: 1080, Enabled: true, GroupID: &group}
	if err := f.s.CreateInbound(f.ctx, ib); err != nil {
		t.Fatal(err)
	}
	change := f.change(t, ib.ID, domain.AccessNone)
	if err := f.s.UpdateInboundAccess(f.ctx, ib.ID, change); err != nil {
		t.Fatal(err)
	}
	ib.Settings.Standalone = &spec.StandaloneAuth{Username: "operator", Password: "secret"}
	wrong := f.groups[1]
	ib.GroupID = &wrong
	if err := f.s.UpdateInbound(f.ctx, ib); !errors.Is(err, ErrStandaloneAccess) {
		t.Fatal("changed legacy group bypassed guard", err)
	}
	ib.GroupID = &group
	if err := f.s.UpdateInbound(f.ctx, ib); err != nil {
		t.Fatal("revoked legacy listener could not become independent", err)
	}
	saved, err := f.s.InboundByID(f.ctx, ib.ID)
	if err != nil || saved.GroupID != nil || saved.Settings.Standalone == nil {
		t.Fatal("legacy group not cleared", err)
	}
	a, err := f.s.InboundAccess(f.ctx, ib.ID)
	if err != nil || a.Policy != domain.AccessNone {
		t.Fatal("conversion restored grant", err)
	}
	saved.Settings.Standalone = nil
	if err := f.s.UpdateInbound(f.ctx, saved); err != nil {
		t.Fatal(err)
	}
	a, err = f.s.InboundAccess(f.ctx, ib.ID)
	if err != nil || a.Policy != domain.AccessNone {
		t.Fatal("switch back restored old group", err)
	}
}
