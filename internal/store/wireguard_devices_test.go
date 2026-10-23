package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
)

func wgDeviceUser(t *testing.T, s *Store, email string) *domain.User {
	t.Helper()
	u := &domain.User{Email: email, Role: domain.RoleUser, Status: "active", UUID: auth.UUID(), SubToken: auth.Token(24)}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestWireGuardDeviceOwnershipRevocationAndRenumbering(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	u := wgDeviceUser(t, s, "devices@example.com")
	other := wgDeviceUser(t, s, "other@example.com")
	first, err := s.CreateWireGuardDevice(ctx, u.ID, "Laptop", 16)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateWireGuardDevice(ctx, u.ID, "Phone", 16)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.UUID == second.UUID || first.Token == second.Token || first.Token == u.SubToken {
		t.Fatal("device credentials not independent")
	}
	if _, err = s.RenameWireGuardDevice(ctx, other.ID, first.ID, "stolen"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-account rename accepted", err)
	}
	if err = s.DeleteWireGuardDevice(ctx, other.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-account delete accepted", err)
	}
	if err = s.ChangeAccountID(ctx, u.ID, 1234, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.WireGuardDeviceByToken(ctx, first.Token)
	if err != nil || got.UserID != 1234 || got.ID != first.ID || got.UUID != first.UUID {
		t.Fatal("renumber altered device identity", err)
	}
	peers, err := s.WireGuardDevicePeers(ctx)
	if err != nil || len(peers[u.AgentID]) != 2 || peers[u.AgentID][0].Address == peers[u.AgentID][1].Address {
		t.Fatal("parent identity/address mapping wrong", err)
	}
	if err = s.DeleteWireGuardDevice(ctx, 1234, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WireGuardDeviceByToken(ctx, first.Token); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked token survived", err)
	}
	third, err := s.CreateWireGuardDevice(ctx, 1234, "Tablet", 16)
	if err != nil || third.ID <= second.ID {
		t.Fatal("device identity reused", err)
	}
	if third.AddressSlot != first.AddressSlot || third.UUID == first.UUID || third.Token == first.Token {
		t.Fatal("free address not reclaimed with fresh identity")
	}
	if err = s.RotateSubToken(ctx, 1234, auth.Token(24)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.WireGuardDevices(ctx, 1234)
	if err != nil || len(rows) != 0 {
		t.Fatal("reset left device credentials", err)
	}
	if _, err = s.WireGuardDeviceByToken(ctx, second.Token); !errors.Is(err, ErrNotFound) {
		t.Fatal("reset token survived", err)
	}
}

func TestWireGuardDeviceLimitsAndPoolFailClosed(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	u := wgDeviceUser(t, s, "limits@example.com")
	for _, name := range []string{"", "  ", "name\nnext", "<script>"} {
		if _, err := s.CreateWireGuardDevice(ctx, u.ID, name, 16); !errors.Is(err, ErrWireGuardDeviceName) {
			t.Fatal("invalid label accepted", err)
		}
	}
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() { defer wait.Done(); _, err := s.CreateWireGuardDevice(ctx, u.ID, "Device", 1); errs <- err }()
	}
	wait.Wait()
	close(errs)
	success, limited := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrWireGuardDeviceLimit) {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || limited != 1 {
		t.Fatal("concurrent creation exceeded limit", success, limited)
	}
	rows, err := s.WireGuardDevices(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteWireGuardDevice(ctx, u.ID, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=65534 WHERE name='wireguard_devices'`); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.CreateWireGuardDevice(ctx, u.ID, "Reclaimed address", 16)
	if err != nil || fresh.ID <= 65534 || fresh.AddressSlot != 1 {
		t.Fatal("identity lifetime incorrectly exhausts address pool", fresh, err)
	}
	if _, err = s.db.ExecContext(ctx, `WITH RECURSIVE slots(n) AS (SELECT 2 UNION ALL SELECT n+1 FROM slots WHERE n<65534)
 INSERT INTO wireguard_devices(address_slot,user_id,name,uuid,token,created_at) SELECT n, ?, 'full', 'uuid-'||n, 'token-'||n, 0 FROM slots`, u.ID); err != nil {
		t.Fatal(err)
	}
	other := wgDeviceUser(t, s, "pool@example.com")
	if _, err = s.CreateWireGuardDevice(ctx, other.ID, "No address", 16); !errors.Is(err, ErrWireGuardDevicePool) {
		t.Fatal("full pool accepted", err)
	}
	rows, err = s.WireGuardDevices(ctx, other.ID)
	if err != nil || len(rows) != 0 {
		t.Fatal("failed allocation persisted", err)
	}
}

func TestWireGuardDevicesRequireFreshBinaryCapability(t *testing.T) {
	s, id, _ := monitorRig(t)
	ctx := context.Background()
	if err := s.ResetPairCode(ctx, id, "pair", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, "pair", "hash", "test", "v0.77.0", "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version       string
		support, want bool
	}{{"v0.76.1", true, false}, {"v0.77.0", false, false}, {"v0.77.0", true, true}} {
		cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{WireGuardDevices: tc.support}}}
		if err := s.TouchNode(ctx, id, tc.version, "", spec.SystemStatus{}, cores, nil); err != nil {
			t.Fatal(err)
		}
		if got := s.WireGuardDevicesCapable(ctx, id); got != tc.want {
			t.Fatalf("version %s support %v: got %v", tc.version, tc.support, got)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen_at=? WHERE id=?`, time.Now().Add(-4*time.Minute).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if s.WireGuardDevicesCapable(ctx, id) {
		t.Fatal("stale capability accepted for registration")
	}
	if !s.WireGuardDevicesSupported(ctx, id) {
		t.Fatal("briefly stale capability removed existing devices")
	}
	if err := s.ResetPairCode(ctx, id, "new-pair", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, "new-pair", "new-hash", "test", "v0.77.0", "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	if s.WireGuardDevicesSupported(ctx, id) {
		t.Fatal("new pairing inherited binary capability")
	}
}

func TestWireGuardDevicesShareHWIDCapacity(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	u := wgDeviceUser(t, s, "shared@example.com")
	limit := 2
	allowed, _, err := s.ClaimHwidDevice(ctx, u.ID, HwidDevice{Hwid: "ordinary-device"}, limit, time.Now())
	if err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	d, err := s.CreateWireGuardDeviceWithHWID(ctx, u.ID, "Phone", 16, &limit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWireGuardDeviceWithHWID(ctx, u.ID, "Tablet", 16, &limit); !errors.Is(err, ErrWireGuardDeviceLimit) {
		t.Fatal("WG exceeded shared quota", err)
	}
	allowed, count, err := s.ClaimHwidDevice(ctx, u.ID, HwidDevice{Hwid: "another-device"}, limit, time.Now())
	if err != nil || allowed || count != 2 {
		t.Fatal("HWID ignored WG reservation", allowed, count, err)
	}
	allowed, count, err = s.ClaimHwidDevice(ctx, u.ID, HwidDevice{Hwid: "ordinary-device"}, 1, time.Now())
	if err != nil || !allowed || count != 2 {
		t.Fatal("existing HWID semantics changed", allowed, count, err)
	}
	if err = s.DeleteWireGuardDevice(ctx, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	allowed, count, err = s.ClaimHwidDevice(ctx, u.ID, HwidDevice{Hwid: "another-device"}, limit, time.Now())
	if err != nil || !allowed || count != 2 {
		t.Fatal("revocation retained reserved slot", allowed, count, err)
	}
}

func TestWireGuardDeviceConcurrentAddressesAndSharedLimit(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	u := wgDeviceUser(t, s, "wg-concurrent@example.com")
	var wait sync.WaitGroup
	devices := make(chan *WireGuardDevice, 8)
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			d, err := s.CreateWireGuardDevice(ctx, u.ID, "Parallel", 16)
			devices <- d
			errs <- err
		}()
	}
	wait.Wait()
	close(devices)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[int64]bool{}
	for d := range devices {
		if d == nil || d.AddressSlot < 1 || d.AddressSlot > 8 || seen[d.AddressSlot] {
			t.Fatal("duplicate/unbounded allocated slot", d)
		}
		seen[d.AddressSlot] = true
	}
	other := wgDeviceUser(t, s, "wg-shared-race@example.com")
	limit := 1
	success := make(chan bool, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := s.CreateWireGuardDeviceWithHWID(ctx, other.ID, "Race", 16, &limit)
		success <- err == nil
	}()
	go func() {
		defer wait.Done()
		ok, _, _ := s.ClaimHwidDevice(ctx, other.ID, HwidDevice{Hwid: "ordinary-race"}, limit, time.Now())
		success <- ok
	}()
	wait.Wait()
	close(success)
	n := 0
	for ok := range success {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatal("two credentials won last shared slot", n)
	}
}
