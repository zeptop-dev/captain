package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Domain is a registered zone the operator owns: node host names, TLS
// names and subscription hosts are expected to live under one.
type Domain struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"` // cloudflare | manual
	CFToken   string    `json:"-"`
	HasToken  bool      `json:"has_token"`
	AutoDNS   bool      `json:"auto_dns"` // create A/AAAA records for names under it
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func scanDomain(row interface{ Scan(...any) error }) (*Domain, error) {
	var d Domain
	var cr, up int64
	var auto int
	if err := row.Scan(&d.ID, &d.Name, &d.Provider, &d.CFToken, &auto, &cr, &up); err != nil {
		return nil, wrapNotFound(err)
	}
	d.HasToken, d.AutoDNS, d.CreatedAt, d.UpdatedAt = d.CFToken != "", auto == 1, unix(cr), unix(up)
	return &d, nil
}

const domainCols = "id, name, provider, cf_token, auto_dns, created_at, updated_at"

func (s *Store) CreateDomain(ctx context.Context, d *Domain) error {
	d.Name = strings.ToLower(strings.TrimSpace(d.Name))
	if d.Provider == "" {
		d.Provider = "cloudflare"
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO domains (name, provider, cf_token, auto_dns, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, d.Name, d.Provider, d.CFToken, boolInt(d.AutoDNS), ts, ts)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return errors.New("domain already registered")
		}
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateDomain(ctx context.Context, d *Domain) error {
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET provider = ?, cf_token = ?, auto_dns = ?, updated_at = ? WHERE id = ?`, d.Provider, d.CFToken, boolInt(d.AutoDNS), now(), d.ID)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id)
	return err
}

func (s *Store) DomainByID(ctx context.Context, id int64) (*Domain, error) {
	return scanDomain(s.db.QueryRowContext(ctx, `SELECT `+domainCols+` FROM domains WHERE id = ?`, id))
}

func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+domainCols+` FROM domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// DomainFor returns the registered domain a host name falls under (the
// longest suffix match), or nil.
func (s *Store) DomainFor(ctx context.Context, host string) (*Domain, error) {
	list, err := s.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	return domainFor(list, host), nil
}

func domainFor(list []Domain, host string) *Domain {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "*."))
	var best *Domain
	for i := range list {
		d := &list[i]
		if host == d.Name || strings.HasSuffix(host, "."+d.Name) {
			if best == nil || len(d.Name) > len(best.Name) {
				best = d
			}
		}
	}
	return best
}

var _ = sql.ErrNoRows
