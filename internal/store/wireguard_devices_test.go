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
		t.Fatal("device address reused", err)
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
	if _, err = s.CreateWireGuardDevice(ctx, u.ID, "No address", 16); !errors.Is(err, ErrWireGuardDevicePool) {
		t.Fatal("address pool wrapped", err)
	}
	rows, err = s.WireGuardDevices(ctx, u.ID)
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
		t.Fatal("stale capability accepted")
	}
}
