package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
)

var ErrSubscriptionProfile = errors.New("invalid subscription profile or template")
var ErrProfileInUse = errors.New("profile or template is in use")

type NamedSubTemplate struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (v NamedSubTemplate) Validate() error {
	if strings.TrimSpace(v.Name) == "" || len(v.Name) > 100 || !spec.Plain(v.Name) || (len(v.Body) > 128<<10 || strings.TrimSpace(v.Body) == "") {
		return ErrSubscriptionProfile
	}
	found := false
	for _, name := range subscription.TemplateNames() {
		found = found || name == v.Format
	}
	if !found {
		return ErrSubscriptionProfile
	}
	if _, err := subscription.Pick(v.Format, "").RenderWith(nil, subscription.Account{}, v.Body); err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}
	return nil
}

type ProfileHWID struct {
	Enabled     *bool   `json:"enabled,omitempty"`
	Require     *bool   `json:"require,omitempty"`
	DeviceLimit *int    `json:"device_limit,omitempty"`
	Announce    *string `json:"announce,omitempty"`
}
type ProfilePage struct {
	Enabled     bool   `json:"enabled"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Accent      string `json:"accent,omitempty"`
}

// AccountInfoSettings controls subscription presentation only, never access or billing.
type AccountInfoSettings struct {
	Traffic bool `json:"traffic"`
	Reset   bool `json:"reset"`
	Expiry  bool `json:"expiry"`
}

func (v AccountInfoSettings) Enabled() bool { return v.Traffic || v.Reset || v.Expiry }

type SubProfileSettings struct {
	Title        string               `json:"title,omitempty"`
	Headers      map[string]string    `json:"headers,omitempty"`
	HWID         *ProfileHWID         `json:"hwid,omitempty"`
	AccountInfo  *AccountInfoSettings `json:"account_info,omitempty"`
	InfoLines    *[]string            `json:"info_lines,omitempty"`
	AutoFlags    *bool                `json:"auto_flags,omitempty"`
	RemarkPrefix string               `json:"remark_prefix,omitempty"`
	Page         *ProfilePage         `json:"page,omitempty"`
}
type SubscriptionProfile struct {
	ID            int64              `json:"id"`
	Name          string             `json:"name"`
	Default       bool               `json:"default"`
	Settings      SubProfileSettings `json:"settings"`
	Templates     map[string]int64   `json:"templates"`
	AssignedUsers int                `json:"assigned_users"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

func (p SubscriptionProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || !spec.Plain(p.Name) || len(p.Settings.Title) > 200 || !spec.Plain(p.Settings.Title) || len(p.Settings.RemarkPrefix) > 200 || !spec.Plain(p.Settings.RemarkPrefix) || len(p.Templates) > 16 {
		return ErrSubscriptionProfile
	}
	for _, id := range p.Templates {
		if id <= 0 {
			return ErrSubscriptionProfile
		}
	}
	if p.Settings.InfoLines != nil {
		if len(*p.Settings.InfoLines) > 10 {
			return ErrSubscriptionProfile
		}
		for _, line := range *p.Settings.InfoLines {
			if len(line) > 500 || !spec.Plain(line) {
				return ErrSubscriptionProfile
			}
		}
	}
	if h := p.Settings.HWID; h != nil {
		if (h.DeviceLimit != nil && (*h.DeviceLimit < 0 || *h.DeviceLimit > 10000)) || (h.Announce != nil && (len(*h.Announce) > 2000 || !spec.Plain(*h.Announce))) {
			return ErrSubscriptionProfile
		}
		if h.Require != nil && *h.Require && h.Enabled != nil && !*h.Enabled {
			return ErrSubscriptionProfile
		}
	}
	if v := p.Settings.Page; v != nil {
		if len(v.Title) > 200 || !spec.Plain(v.Title) || len(v.Description) > 2000 || (v.Accent != "" && !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(v.Accent)) {
			return ErrSubscriptionProfile
		}
	}
	if len(p.Settings.Headers) > 12 {
		return ErrSubscriptionProfile
	}
	seen := map[string]bool{}
	for k, v := range p.Settings.Headers {
		key := http.CanonicalHeaderKey(k)
		if seen[key] || len(v) > 2000 || !spec.Plain(v) {
			return ErrSubscriptionProfile
		}
		seen[key] = true
		switch key {
		case "Profile-Title", "Announce":
		case "Profile-Update-Interval":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 168 {
				return ErrSubscriptionProfile
			}
		case "Profile-Web-Page-Url", "Support-Url", "Announce-Url":
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
				return ErrSubscriptionProfile
			}
		default:
			return fmt.Errorf("unsupported profile header %q", k)
		}
	}
	return nil
}
func (s *Store) NamedSubTemplates(ctx context.Context) ([]NamedSubTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,format,body,updated_at FROM subscription_templates ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NamedSubTemplate{}
	for rows.Next() {
		var v NamedSubTemplate
		var updated int64
		if err = rows.Scan(&v.ID, &v.Name, &v.Format, &v.Body, &updated); err != nil {
			return nil, err
		}
		v.UpdatedAt = unix(updated)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) NamedSubTemplate(ctx context.Context, id int64) (*NamedSubTemplate, error) {
	var v NamedSubTemplate
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT id,name,format,body,updated_at FROM subscription_templates WHERE id=?`, id).Scan(&v.ID, &v.Name, &v.Format, &v.Body, &updated)
	v.UpdatedAt = unix(updated)
	return &v, wrapNotFound(err)
}
func (s *Store) SaveNamedSubTemplate(ctx context.Context, v *NamedSubTemplate) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO subscription_templates(name,format,body,updated_at) VALUES(?,?,?,?)`, v.Name, v.Format, v.Body, now())
		if err != nil {
			return err
		}
		v.ID, err = res.LastInsertId()
		return err
	}
	// Format is immutable once created; a profile binding must retain its format.
	res, err := s.db.ExecContext(ctx, `UPDATE subscription_templates SET name=?,body=?,updated_at=? WHERE id=? AND format=?`, v.Name, v.Body, now(), v.ID, v.Format)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteNamedSubTemplate(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_profile_templates WHERE template_id=?)`, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrProfileInUse
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM subscription_templates WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
func scanSubProfile(row interface{ Scan(...any) error }) (*SubscriptionProfile, error) {
	p := &SubscriptionProfile{Templates: map[string]int64{}}
	var b string
	var updated int64
	if err := row.Scan(&p.ID, &p.Name, &p.Default, &b, &updated, &p.AssignedUsers); err != nil {
		return nil, wrapNotFound(err)
	}
	p.UpdatedAt = unix(updated)
	if err := json.Unmarshal([]byte(b), &p.Settings); err != nil {
		return nil, err
	}
	return p, nil
}

const profileCols = `id,name,is_default,settings_json,updated_at,(SELECT COUNT(*) FROM user_subscription_profiles u WHERE u.profile_id=subscription_profiles.id)`

func (s *Store) profileTemplates(ctx context.Context, p *SubscriptionProfile) error {
	rows, err := s.db.QueryContext(ctx, `SELECT format,template_id FROM subscription_profile_templates WHERE profile_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		var id int64
		if err = rows.Scan(&f, &id); err != nil {
			return err
		}
		p.Templates[f] = id
	}
	return rows.Err()
}
func (s *Store) SubscriptionProfiles(ctx context.Context) ([]SubscriptionProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+profileCols+` FROM subscription_profiles ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	out := []SubscriptionProfile{}
	for rows.Next() {
		p, err := scanSubProfile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err = s.profileTemplates(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Store) SubscriptionProfile(ctx context.Context, id int64) (*SubscriptionProfile, error) {
	p, err := scanSubProfile(s.db.QueryRowContext(ctx, `SELECT `+profileCols+` FROM subscription_profiles WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return p, s.profileTemplates(ctx, p)
}
func (s *Store) SubscriptionProfileForUser(ctx context.Context, id int64) (*SubscriptionProfile, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT profile_id FROM user_subscription_profiles WHERE user_id=?),(SELECT id FROM subscription_profiles WHERE is_default=1))`, id).Scan(&v)
	if err != nil {
		return nil, err
	}
	if !v.Valid {
		return nil, nil
	}
	return s.SubscriptionProfile(ctx, v.Int64)
}
func (s *Store) SaveSubscriptionProfile(ctx context.Context, p *SubscriptionProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for format, id := range p.Templates {
		var found string
		if err = tx.QueryRowContext(ctx, `SELECT format FROM subscription_templates WHERE id=?`, id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return ErrSubscriptionProfile
		} else if err != nil {
			return err
		}
		if format != found {
			return ErrSubscriptionProfile
		}
	}
	if p.Default {
		if _, err = tx.ExecContext(ctx, `UPDATE subscription_profiles SET is_default=0 WHERE is_default=1`); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(p.Settings)
	if p.ID == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO subscription_profiles(name,is_default,settings_json,updated_at) VALUES(?,?,?,?)`, p.Name, boolInt(p.Default), string(b), now())
		if e != nil {
			return e
		}
		p.ID, e = res.LastInsertId()
		if e != nil {
			return e
		}
	} else {
		res, e := tx.ExecContext(ctx, `UPDATE subscription_profiles SET name=?,is_default=?,settings_json=?,updated_at=? WHERE id=?`, p.Name, boolInt(p.Default), string(b), now(), p.ID)
		if e != nil {
			return e
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_profile_templates WHERE profile_id=?`, p.ID); err != nil {
		return err
	}
	for format, id := range p.Templates {
		if _, err = tx.ExecContext(ctx, `INSERT INTO subscription_profile_templates(profile_id,format,template_id) VALUES(?,?,?)`, p.ID, format, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteSubscriptionProfile(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_subscription_profiles WHERE profile_id=?) OR EXISTS(SELECT 1 FROM subscription_profiles WHERE id=? AND is_default=1)`, id, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrProfileInUse
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM subscription_profiles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
func (s *Store) AssignSubscriptionProfile(ctx context.Context, userID int64, profileID *int64) error {
	if profileID == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM user_subscription_profiles WHERE user_id=?`, userID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_subscription_profiles(user_id,profile_id) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET profile_id=excluded.profile_id`, userID, *profileID)
	return err
}
func (s *Store) AssignedSubscriptionProfile(ctx context.Context, userID int64) (*int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT profile_id FROM user_subscription_profiles WHERE user_id=?`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}
