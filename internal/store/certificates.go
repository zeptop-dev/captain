package store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Certificate is an operator-supplied PEM pair pushed to nodes whose
// inbounds use one of its names.
type Certificate struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Names     []string  `json:"names"`
	CertPEM   string    `json:"cert_pem,omitempty"`
	KeyPEM    string    `json:"-"`
	NotAfter  time.Time `json:"not_after"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ParseCertificate validates a PEM pair and reads its names and expiry.
// domain may be blank: the leaf's first DNS name is used.
func ParseCertificate(domain, certPEM, keyPEM string) (*Certificate, error) {
	certPEM, keyPEM = strings.TrimSpace(certPEM)+"\n", strings.TrimSpace(keyPEM)+"\n"
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, errors.New("certificate and key do not parse as a pair: " + err.Error())
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	names := append([]string{}, leaf.DNSNames...)
	if len(names) == 0 && leaf.Subject.CommonName != "" {
		names = []string{leaf.Subject.CommonName}
	}
	for i := range names {
		names[i] = strings.ToLower(names[i])
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		if len(names) == 0 {
			return nil, errors.New("certificate has no DNS names; give a domain")
		}
		domain = names[0]
	}
	if !containsName(names, domain) {
		names = append([]string{domain}, names...)
	}
	return &Certificate{Domain: domain, Names: names, CertPEM: certPEM, KeyPEM: keyPEM, NotAfter: leaf.NotAfter}, nil
}

func containsName(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

// CoversName reports whether a certificate name (exact or *.wildcard)
// matches a server name.
func CoversName(pattern, name string) bool {
	pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	if pattern == "" || name == "" {
		return false
	}
	if pattern == name {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(name, suffix) && !strings.Contains(strings.TrimSuffix(name, suffix), ".")
	}
	return false
}

// Covers reports whether any of the certificate's names matches.
func (c *Certificate) Covers(name string) bool {
	for _, n := range c.Names {
		if CoversName(n, name) {
			return true
		}
	}
	return false
}

// UpsertCertificate stores or replaces the pair for c.Domain.
func (s *Store) UpsertCertificate(ctx context.Context, c *Certificate) error {
	names, _ := json.Marshal(c.Names)
	if c.Source == "" {
		c.Source = "upload"
	}
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO certificates (domain, names_json, cert_pem, key_pem, not_after, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET names_json = excluded.names_json, cert_pem = excluded.cert_pem, key_pem = excluded.key_pem, not_after = excluded.not_after, source = excluded.source, updated_at = excluded.updated_at`,
		c.Domain, string(names), c.CertPEM, c.KeyPEM, c.NotAfter.Unix(), c.Source, ts, ts)
	return err
}

func (s *Store) ListCertificates(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, domain, names_json, cert_pem, key_pem, not_after, source, created_at, updated_at FROM certificates ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Certificate{}
	for rows.Next() {
		var c Certificate
		var names string
		var na, cr, up int64
		if err := rows.Scan(&c.ID, &c.Domain, &names, &c.CertPEM, &c.KeyPEM, &na, &c.Source, &cr, &up); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(names), &c.Names)
		c.NotAfter, c.CreatedAt, c.UpdatedAt = unix(na), unix(cr), unix(up)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCertificate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM certificates WHERE id = ?`, id)
	return err
}

// CertificatesFor returns the certificates covering any of the names,
// each at most once.
func (s *Store) CertificatesFor(ctx context.Context, names []string) ([]Certificate, error) {
	all, err := s.ListCertificates(ctx)
	if err != nil || len(names) == 0 {
		return nil, err
	}
	var out []Certificate
	for _, c := range all {
		for _, n := range names {
			if c.Covers(n) {
				out = append(out, c)
				break
			}
		}
	}
	return out, nil
}
