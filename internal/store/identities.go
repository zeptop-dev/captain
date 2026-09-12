package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Identity links an external login (OIDC subject) to a user.
type Identity struct {
	Provider  string    `json:"provider"`
	Subject   string    `json:"subject"`
	UserID    int64     `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// UserByIdentity finds the user linked to a provider subject.
func (s *Store) UserByIdentity(ctx context.Context, provider, subject string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM identities WHERE provider = ? AND subject = ?`, provider, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// LinkIdentity attaches a provider subject to a user (replacing an older
// link of the same provider for that user).
func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider, subject, email string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM identities WHERE user_id = ? AND provider = ?`, userID, provider); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO identities (provider, subject, user_id, email, created_at) VALUES (?, ?, ?, ?, ?)`, provider, subject, userID, email, now())
	return err
}

// UnlinkIdentity removes a provider link from a user.
func (s *Store) UnlinkIdentity(ctx context.Context, userID int64, provider string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM identities WHERE user_id = ? AND provider = ?`, userID, provider)
	return err
}

// IdentitiesByUser lists a user's linked logins.
func (s *Store) IdentitiesByUser(ctx context.Context, userID int64) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, subject, user_id, email, created_at FROM identities WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var i Identity
		var created int64
		if err := rows.Scan(&i.Provider, &i.Subject, &i.UserID, &i.Email, &created); err != nil {
			return nil, err
		}
		i.CreatedAt = time.Unix(created, 0)
		out = append(out, i)
	}
	return out, rows.Err()
}

// OIDCProvider is one external login configured by the admin.
type OIDCProvider struct {
	ID           string   `json:"id"`            // short slug used in URLs, e.g. "casdoor"
	Name         string   `json:"name"`          // button label
	Issuer       string   `json:"issuer"`        // discovery base URL
	ClientID     string   `json:"client_id"`     //
	ClientSecret string   `json:"client_secret"` //
	Scopes       []string `json:"scopes"`        // default openid profile email
	// TrustEmail treats the provider's email as verified even without an
	// email_verified claim (fine for a provider you run yourself).
	TrustEmail bool `json:"trust_email"`
	// AutoRegister creates accounts for unknown users even when password
	// registration is closed.
	AutoRegister bool `json:"auto_register"`
}

// OIDCSettings is the settings value under SettingOIDC.
type OIDCSettings struct {
	Providers []OIDCProvider `json:"providers"`
	// PasswordLogin false hides password sign-in on the portal once an
	// external login exists.
	PasswordLogin *bool `json:"password_login,omitempty"`
}

// SettingOIDC is the settings key.
const SettingOIDC = "oidc"
