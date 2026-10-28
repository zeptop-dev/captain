package store

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/net/idna"
)

var ErrInvalidProbeOrigin = errors.New("probe origin must be one DNS hostname or IP address without a scheme, port, path or wildcard")

// NormalizeProbeOrigin identifies a hostname, not a URL or a socket endpoint.
// Callers handling HTTP Host must separately remove its optional port/brackets.
// DNS case, IDNA spellings, the root dot and IP compression share one key.
func NormalizeProbeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 1024 {
		return "", ErrInvalidProbeOrigin
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return "", ErrInvalidProbeOrigin
		}
		return addr.String(), nil
	}
	name, err := idna.Lookup.ToASCII(raw)
	if err != nil {
		return "", ErrInvalidProbeOrigin
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name == "" || len(name) > 253 {
		return "", ErrInvalidProbeOrigin
	}
	if addr, err := netip.ParseAddr(name); err == nil {
		return addr.String(), nil
	}
	// Reject non-canonical numeric IPv4 spellings (for example 127.1), which
	// browsers may interpret as a different host than a DNS-name comparison.
	numeric := true
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidProbeOrigin
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrInvalidProbeOrigin
			}
		}
		number, hex := label, strings.HasPrefix(label, "0x")
		if hex {
			number = number[2:]
		}
		if number == "" {
			numeric = false
		}
		for _, c := range number {
			if !(c >= '0' && c <= '9' || hex && c >= 'a' && c <= 'f') {
				numeric = false
			}
		}
	}
	if numeric {
		return "", ErrInvalidProbeOrigin
	}
	return name, nil
}

// RememberProbeOrigins atomically appends permanent probe-only hostnames.
// There is deliberately no removal API: cached third-party code may still run
// on a hostname after administrators remove its theme, settings or entire site.
func (s *Store) RememberProbeOrigins(ctx context.Context, hostnames []string) error {
	hosts := make(map[string]struct{}, len(hostnames))
	for _, hostname := range hostnames {
		host, err := NormalizeProbeOrigin(hostname)
		if err != nil {
			return err
		}
		hosts[host] = struct{}{}
	}
	if len(hosts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for host := range hosts {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO probe_origins(hostname) VALUES(?)`, host); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) IsProbeOrigin(ctx context.Context, hostname string) (bool, error) {
	host, err := NormalizeProbeOrigin(hostname)
	if err != nil {
		return false, err
	}
	var remembered bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM probe_origins WHERE hostname=?)`, host).Scan(&remembered)
	return remembered, err
}
