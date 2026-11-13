package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/zeptop-dev/captain/internal/agentendpoint"
	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/auth"
)

type AgentEndpoint struct {
	EverDefault     bool `json:"-"`
	UnknownUsers    int  `json:"unknown_users"`
	ConfiguredUsers int  `json:"configured_users"`
	agenthttps.Config
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Default  bool   `json:"default"`
	Users    int    `json:"users"`
	Pending  int    `json:"pending"`
}

const endpointCols = "id,name,url,listen,enabled,certificate_id,revision,is_default,was_default"

func scanAgentEndpoint(row interface{ Scan(...any) error }) (AgentEndpoint, error) {
	var v AgentEndpoint
	err := row.Scan(&v.ID, &v.Name, &v.URL, &v.Listen, &v.Enabled, &v.CertificateID, &v.Revision, &v.Default, &v.EverDefault)
	return v, wrapNotFound(err)
}
func (s *Store) AgentEndpoints(ctx context.Context) ([]AgentEndpoint, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+endpointCols+" FROM agent_endpoints ORDER BY is_default DESC,name,id")
	if err != nil {
		return nil, err
	}
	out := []AgentEndpoint{}
	for rows.Next() {
		v, e := scanAgentEndpoint(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		v := &out[i]
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM nodes n JOIN agent_node_connections c ON c.node_id=n.id AND c.token_hash=n.token_hash WHERE c.current_url=?`, v.URL).Scan(&v.Users)
		if err != nil {
			return nil, err
		}
		if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM nodes n LEFT JOIN agent_node_connections c ON c.node_id=n.id WHERE ?=1 AND n.token_hash IS NOT NULL AND (coalesce(c.current_url,'')='' OR coalesce(c.token_hash,'')<>n.token_hash)`, v.EverDefault).Scan(&v.UnknownUsers); err != nil {
			return nil, err
		}
		if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM nodes n LEFT JOIN agent_node_connections c ON c.node_id=n.id WHERE n.agent_url=? OR c.endpoint_id=?`, v.URL, v.ID).Scan(&v.ConfiguredUsers); err != nil {
			return nil, err
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_connection_migrations WHERE (target_url=? OR source_url=?) AND status IN ('queued','issued','verified')`, v.URL, v.URL).Scan(&v.Pending)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func endpointReferences(ctx context.Context, tx *sql.Tx, v AgentEndpoint) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM nodes x LEFT JOIN agent_node_connections c ON c.node_id=x.id WHERE
 x.agent_url=? OR c.endpoint_id=? OR (c.token_hash=x.token_hash AND c.current_url=?) OR
 (x.token_hash<>'' AND (coalesce(c.current_url,'')='' OR coalesce(c.token_hash,'')<>coalesce(x.token_hash,'')) AND ?=1))+
 (SELECT count(*) FROM agent_connection_migrations WHERE (source_url=? OR target_url=?) AND status IN ('queued','issued','verified'))`, v.URL, v.ID, v.URL, v.EverDefault, v.URL, v.URL).Scan(&n)
	return n, err
}
func (s *Store) SaveAgentEndpoint(ctx context.Context, v AgentEndpoint) (AgentEndpoint, error) {
	v.Name = strings.TrimSpace(v.Name)
	if len(v.Name) == 0 || len(v.Name) > 100 || strings.ContainsAny(v.Name, "\r\n\x00") {
		return v, fmt.Errorf("%w: invalid entry name", ErrAgentHTTPSInvalid)
	}
	clean, err := agenthttps.Normalize(v.Config)
	if err != nil {
		return v, fmt.Errorf("%w: %v", ErrAgentHTTPSInvalid, err)
	}
	v.Config = clean
	if v.Default && !v.Enabled {
		return v, fmt.Errorf("%w: default entry must be enabled", ErrAgentHTTPSInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if v.ID == "" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_endpoints").Scan(&count); err != nil {
			return v, err
		}
		if count >= 16 {
			return v, fmt.Errorf("%w: at most 16 entries", ErrAgentHTTPSInvalid)
		}
		v.ID = auth.Token(18)
	} else {
		old, e := scanAgentEndpoint(tx.QueryRowContext(ctx, "SELECT "+endpointCols+" FROM agent_endpoints WHERE id=?", v.ID))
		if e != nil {
			return v, e
		}
		v.EverDefault = old.EverDefault
		if old.Default {
			v.Default = true
		}
		if old.Revision != v.Revision {
			return v, ErrAgentHTTPSRevision
		}
		if old.URL != v.URL || old.Listen != v.Listen || old.Enabled && !v.Enabled {
			refs, e := endpointReferences(ctx, tx, old)
			if e != nil {
				return v, e
			}
			if refs > 0 {
				return v, fmt.Errorf("%w: entry is still referenced by %d nodes or migrations", ErrAgentHTTPSInvalid, refs)
			}
		}
	}
	if v.Default && !v.Enabled {
		return v, fmt.Errorf("%w: choose another default before disabling this entry", ErrAgentHTTPSInvalid)
	}
	if v.Enabled {
		rows, e := tx.QueryContext(ctx, "SELECT url,listen FROM agent_endpoints WHERE enabled=1 AND id<>?", v.ID)
		if e != nil {
			return v, e
		}
		collision := false
		for rows.Next() {
			var otherURL, listen string
			if e = rows.Scan(&otherURL, &listen); e != nil {
				rows.Close()
				return v, e
			}
			if listen == v.Listen && canonicalEndpointHost(otherURL) == canonicalEndpointHost(v.URL) {
				collision = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return v, e
		}
		if collision {
			return v, fmt.Errorf("%w: hostname already uses this listener", ErrAgentHTTPSInvalid)
		}
		cert, e := scanCert(tx.QueryRowContext(ctx, "SELECT "+certCols+" FROM certificates WHERE id=?", v.CertificateID))
		if e != nil {
			return v, e
		}
		if e = agenthttps.Validate(v.Config, []byte(cert.CertPEM), []byte(cert.KeyPEM)); e != nil {
			return v, fmt.Errorf("%w: %v", ErrAgentHTTPSInvalid, e)
		}
	}
	// One URL has one identity. A shared port is intentional; TLS routes by SNI.
	var other string
	e := tx.QueryRowContext(ctx, "SELECT id FROM agent_endpoints WHERE url=? AND id<>?", v.URL, v.ID).Scan(&other)
	if e == nil {
		return v, fmt.Errorf("%w: address already exists", ErrAgentHTTPSInvalid)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return v, e
	}
	if v.Default {
		if _, err = tx.ExecContext(ctx, "UPDATE agent_endpoints SET is_default=0,revision=? WHERE is_default=1 AND id<>?", auth.Token(24), v.ID); err != nil {
			return v, err
		}
	}
	v.Revision = auth.Token(24)
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_endpoints(id,name,url,listen,enabled,certificate_id,revision,is_default,was_default) VALUES(?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,url=excluded.url,listen=excluded.listen,enabled=excluded.enabled,certificate_id=excluded.certificate_id,revision=excluded.revision,is_default=excluded.is_default,was_default=excluded.was_default`, v.ID, v.Name, v.URL, v.Listen, v.Enabled, v.CertificateID, v.Revision, v.Default, v.EverDefault || v.Default)
	if err != nil {
		return v, err
	}
	if v.Default {
		raw, e := json.Marshal(AgentHTTPSSettings{Config: v.Config, Revision: v.Revision})
		if e != nil {
			return v, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json)VALUES(?,?) ON CONFLICT(key)DO UPDATE SET value_json=excluded.value_json`, SettingAgentHTTPS, string(raw)); e != nil {
			return v, e
		}
	}
	return v, tx.Commit()
}
func (s *Store) DeleteAgentEndpoint(ctx context.Context, id, revision string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := scanAgentEndpoint(tx.QueryRowContext(ctx, "SELECT "+endpointCols+" FROM agent_endpoints WHERE id=?", id))
	if err != nil {
		return err
	}
	if v.Revision != revision {
		return ErrAgentHTTPSRevision
	}
	refs, err := endpointReferences(ctx, tx, v)
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("%w: entry has %d references; migrate nodes first", ErrAgentHTTPSInvalid, refs)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM agent_endpoints WHERE id=?", id); err != nil {
		return err
	}
	if v.Default {
		raw, _ := json.Marshal(AgentHTTPSSettings{Config: agenthttps.Config{Listen: ":2096"}, Revision: auth.Token(24)})
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json)VALUES(?,?) ON CONFLICT(key)DO UPDATE SET value_json=excluded.value_json`, SettingAgentHTTPS, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DefaultAgentURL(ctx context.Context, fallback string) string {
	var u string
	if s.db.QueryRowContext(ctx, "SELECT url FROM agent_endpoints WHERE is_default=1 AND enabled=1").Scan(&u) == nil {
		return u
	}
	return fallback
}
func (s *Store) AgentEndpointCertificates(ctx context.Context) ([]AgentEndpoint, map[int64]*Certificate, error) {
	entries, err := s.AgentEndpoints(ctx)
	if err != nil {
		return nil, nil, err
	}
	certs := map[int64]*Certificate{}
	for _, v := range entries {
		if v.Enabled {
			c, e := s.CertificateByID(ctx, v.CertificateID)
			if e != nil {
				return nil, nil, e
			}
			certs[v.CertificateID] = c
		}
	}
	return entries, certs, nil
}

// AgentOriginHost compares the actual HTTPS authority without trusting proxy headers.
func AgentOriginHost(origin, host string) bool {
	u, err := agentendpoint.NormalizeURL("https://" + host)
	if err != nil {
		return false
	}
	return strings.TrimSuffix(u, ":443") == strings.TrimSuffix(origin, ":443")
}

func canonicalEndpointHost(origin string) string {
	u, e := url.Parse(origin)
	if e != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, e := netip.ParseAddr(host); e == nil {
		return ip.Unmap().String()
	}
	return host
}
