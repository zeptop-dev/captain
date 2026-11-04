package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/auth"
)

const SettingAgentHTTPS = "agent_https"

var ErrAgentHTTPSRevision = errors.New("Agent HTTPS settings changed; reload before saving")
var ErrAgentHTTPSInvalid = errors.New("invalid Agent HTTPS settings")
var ErrCertificateInUse = errors.New("certificate is still selected or in use by Agent HTTPS; clear its selection and stop any retained listener first")

// AgentHTTPSSettings is deployment intent, independent of per-node installation
// preferences. It never changes the URL or credentials of an existing agent.
type AgentHTTPSSettings struct {
	agenthttps.Config
	Revision string `json:"revision"`
}

type settingsReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readAgentHTTPS(ctx context.Context, q settingsReader) (AgentHTTPSSettings, error) {
	v := AgentHTTPSSettings{Config: agenthttps.Config{Listen: ":2096"}, Revision: "0"}
	var raw string
	err := q.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, SettingAgentHTTPS).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

// AgentHTTPSnapshot reads intent and its certificate from one database snapshot.
// A renewal or reset cannot pair a new selection with an unrelated PEM pair.
func (s *Store) AgentHTTPSnapshot(ctx context.Context) (AgentHTTPSSettings, *Certificate, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AgentHTTPSSettings{}, nil, err
	}
	defer tx.Rollback()
	v, err := readAgentHTTPS(ctx, tx)
	if err != nil {
		return v, nil, err
	}
	var cert *Certificate
	if v.Enabled {
		cert, err = scanCert(tx.QueryRowContext(ctx, `SELECT `+certCols+` FROM certificates WHERE id = ?`, v.CertificateID))
		if err != nil {
			return v, nil, err
		}
	}
	return v, cert, tx.Commit()
}

func (s *Store) AgentHTTPSSettings(ctx context.Context) (AgentHTTPSSettings, error) {
	return readAgentHTTPS(ctx, s.db)
}

// SaveAgentHTTPS checks both revision and the currently selected certificate in
// the write transaction. Certificate deletion uses the same transaction boundary.
func (s *Store) SaveAgentHTTPS(ctx context.Context, in AgentHTTPSSettings) (AgentHTTPSSettings, error) {
	clean, err := agenthttps.Normalize(in.Config)
	if err != nil {
		return in, fmt.Errorf("%w: %v", ErrAgentHTTPSInvalid, err)
	}
	in.Config = clean
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return in, err
	}
	defer tx.Rollback()
	old, err := readAgentHTTPS(ctx, tx)
	if err != nil {
		return in, err
	}
	if in.Revision == "" || in.Revision != old.Revision {
		return in, ErrAgentHTTPSRevision
	}
	if in.CertificateID > 0 {
		cert, err := scanCert(tx.QueryRowContext(ctx, `SELECT `+certCols+` FROM certificates WHERE id = ?`, in.CertificateID))
		if errors.Is(err, ErrNotFound) {
			return in, fmt.Errorf("%w: selected certificate no longer exists", ErrAgentHTTPSInvalid)
		}
		if err != nil {
			return in, err
		}
		if in.Enabled {
			if err = agenthttps.Validate(in.Config, []byte(cert.CertPEM), []byte(cert.KeyPEM)); err != nil {
				return in, fmt.Errorf("%w: %v", ErrAgentHTTPSInvalid, err)
			}
		}
	}
	if in.Config == old.Config {
		return old, tx.Commit()
	}
	in.Revision = auth.Token(24)
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json`, SettingAgentHTTPS, string(raw))
	if err != nil {
		return in, err
	}
	return in, tx.Commit()
}
