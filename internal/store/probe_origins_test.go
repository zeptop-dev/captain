package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestNormalizeProbeOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		" Probe.Example.COM. ": "probe.example.com",
		"BÜCHER.example":       "xn--bcher-kva.example",
		"localhost":            "localhost",
		"192.0.2.10":           "192.0.2.10",
		"192.0.2.10.":          "192.0.2.10",
		"2001:0DB8:0:0::1":     "2001:db8::1",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := NormalizeProbeOrigin(raw)
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
	for _, raw := range []string{"", " ", ".", "example.com..", "https://probe.example.com", "probe.example.com:443", "probe.example.com/path", "probe.example.com?x=1", "probe.example.com#fragment", "user@probe.example.com", "*.example.com", "[2001:db8::1]", "[2001:db8::1]:443", "fe80::1%eth0", "127.1", "192.000.2.10", "2130706433", "0x7f000001", "0x7f.0.0.1", "0177.0.0.1", "bad_name.example", "-bad.example", "bad-.example", "example.com\n/path", strings.Repeat("a", 64) + ".example", strings.Repeat("a.", 127) + "example"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NormalizeProbeOrigin(raw); !errors.Is(err, ErrInvalidProbeOrigin) {
				t.Fatalf("accepted invalid hostname %q: %v", raw, err)
			}
		})
	}
}

func TestProbeOriginsAppendAtomicallyAndSurviveReset(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "origins.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	if err = s.RememberProbeOrigins(ctx, []string{"Probe.Example.COM.", "probe.example.com", "2001:0db8::1"}); err != nil {
		t.Fatal(err)
	}
	if err = s.RememberProbeOrigins(ctx, []string{"PROBE.EXAMPLE.COM", "2001:db8::1"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = conn.QueryRow(`SELECT count(*) FROM probe_origins`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate registration was not idempotent", count, err)
	}
	for _, host := range []string{"probe.example.com", "PROBE.EXAMPLE.COM.", "2001:db8:0:0::1"} {
		if ok, err := s.IsProbeOrigin(ctx, host); err != nil || !ok {
			t.Fatal("registered hostname not isolated", host, ok, err)
		}
	}
	if ok, err := s.IsProbeOrigin(ctx, "unused.example.com"); err != nil || ok {
		t.Fatal("unknown hostname was remembered", ok, err)
	}
	if err = s.RememberProbeOrigins(ctx, []string{"new.example.com", "https://invalid.example.com"}); !errors.Is(err, ErrInvalidProbeOrigin) {
		t.Fatal("invalid batch accepted", err)
	}
	if ok, err := s.IsProbeOrigin(ctx, "new.example.com"); err != nil || ok {
		t.Fatal("invalid batch partially saved", ok, err)
	}
	if _, err = s.IsProbeOrigin(ctx, "probe.example.com:443"); !errors.Is(err, ErrInvalidProbeOrigin) {
		t.Fatal("invalid lookup accepted", err)
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test", Role: domain.RoleAdmin, Status: "active"}
	if err = s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting(ctx, SettingProbe, ProbeSettings{Hosts: []string{"probe.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(`SELECT count(*) FROM settings WHERE key=?`, SettingProbe).Scan(&count); err != nil || count != 0 {
		t.Fatal("reset retained ordinary probe settings", err)
	}
	for _, host := range []string{"probe.example.com", "2001:db8::1"} {
		if ok, err := s.IsProbeOrigin(ctx, host); err != nil || !ok {
			t.Fatal("reset removed the permanent probe origin boundary", host, ok, err)
		}
	}
	if err = s.RememberProbeOrigins(ctx, []string{"new.example.com"}); err != nil {
		t.Fatal("cannot register an origin after reset", err)
	}
	if err = conn.QueryRow(`SELECT count(*) FROM probe_origins`).Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
}
