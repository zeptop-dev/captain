package store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Certificate is an operator-supplied PEM pair pushed to nodes whose
// inbounds use one of its names.
type Certificate struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"` // display name; defaults to Domain
	Domain    string    `json:"domain"`
	Names     []string  `json:"names"`
	CertPEM   string    `json:"cert_pem,omitempty"`
	KeyPEM    string    `json:"-"`
	NotAfter  time.Time `json:"not_after"`
	Source    string    `json:"source"` // upload | webhook | acme
	Issuer    string    `json:"issuer"` // CA common name for acme / parsed from the leaf
	Renewals  int       `json:"renewals"`
	LastError string    `json:"last_error"`
	AutoRenew bool      `json:"auto_renew"`
	DomainID  *int64    `json:"domain_id"` // DNS authorisation used for acme issuance
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
	return &Certificate{Domain: domain, Names: names, CertPEM: certPEM, KeyPEM: keyPEM, NotAfter: leaf.NotAfter, Issuer: leaf.Issuer.CommonName, AutoRenew: true}, nil
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
	if c.Name == "" {
		c.Name = c.Domain
	}
	ts := now()
	// A re-issue for the same primary name replaces the record; renewals
	// count up and the display name survives unless a new one is given.
	_, err := s.db.ExecContext(ctx, `INSERT INTO certificates (domain, name, names_json, cert_pem, key_pem, not_after, source, issuer, auto_renew, domain_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET name = CASE WHEN excluded.name = excluded.domain THEN certificates.name ELSE excluded.name END, names_json = excluded.names_json, cert_pem = excluded.cert_pem, key_pem = excluded.key_pem, not_after = excluded.not_after, source = excluded.source, issuer = excluded.issuer, auto_renew = excluded.auto_renew, domain_id = COALESCE(excluded.domain_id, certificates.domain_id), last_error = '', renewals = certificates.renewals + 1, updated_at = excluded.updated_at`,
		c.Domain, c.Name, string(names), c.CertPEM, c.KeyPEM, c.NotAfter.Unix(), c.Source, c.Issuer, boolInt(c.AutoRenew), nullInt64(c.DomainID), ts, ts)
	if err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx, `SELECT id FROM certificates WHERE domain = ?`, c.Domain).Scan(&c.ID)
}

const certCols = "id, name, domain, names_json, cert_pem, key_pem, not_after, source, issuer, renewals, last_error, auto_renew, domain_id, created_at, updated_at"

func scanCert(row interface{ Scan(...any) error }) (*Certificate, error) {
	var c Certificate
	var names string
	var na, cr, up int64
	var ar int
	var did sql.NullInt64
	if err := row.Scan(&c.ID, &c.Name, &c.Domain, &names, &c.CertPEM, &c.KeyPEM, &na, &c.Source, &c.Issuer, &c.Renewals, &c.LastError, &ar, &did, &cr, &up); err != nil {
		return nil, wrapNotFound(err)
	}
	_ = json.Unmarshal([]byte(names), &c.Names)
	c.NotAfter, c.CreatedAt, c.UpdatedAt, c.AutoRenew, c.DomainID = unix(na), unix(cr), unix(up), ar == 1, int64Ptr(did)
	if c.Name == "" {
		c.Name = c.Domain
	}
	return &c, nil
}

func (s *Store) CertificateByID(ctx context.Context, id int64) (*Certificate, error) {
	return scanCert(s.db.QueryRowContext(ctx, `SELECT `+certCols+` FROM certificates WHERE id = ?`, id))
}

// UpdateCertificateMeta changes the display name and renewal flag.
func (s *Store) UpdateCertificateMeta(ctx context.Context, id int64, name string, autoRenew bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE certificates SET name = ?, auto_renew = ?, updated_at = ? WHERE id = ?`, name, boolInt(autoRenew), now(), id)
	return err
}

// SetCertificateError records a failed renewal attempt.
func (s *Store) SetCertificateError(ctx context.Context, id int64, msg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE certificates SET last_error = ?, updated_at = ? WHERE id = ?`, msg, now(), id)
	return err
}

// CertificatesDue lists ACME certificates that auto-renew and expire before t.
func (s *Store) CertificatesDue(ctx context.Context, t time.Time) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certCols+` FROM certificates WHERE source = 'acme' AND auto_renew = 1 AND not_after < ? ORDER BY not_after`, t.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Certificate{}
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) ListCertificates(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certCols+` FROM certificates ORDER BY name, domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Certificate{}
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
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
