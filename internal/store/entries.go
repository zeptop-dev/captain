package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

const entryCols = "e.id, e.name, e.inbound_id, e.chain_id, e.display_host, e.display_port, e.rate, e.sort, e.enabled, e.tags, e.region"

func scanEntry(row interface{ Scan(...any) error }) (*domain.Entry, error) {
	var e domain.Entry
	var chain sql.NullInt64
	var enabled int
	var tags string
	if err := row.Scan(&e.ID, &e.Name, &e.InboundID, &chain, &e.DisplayHost, &e.DisplayPort, &e.Rate, &e.Sort, &enabled, &tags, &e.Region); err != nil {
		return nil, wrapNotFound(err)
	}
	e.ChainID = int64Ptr(chain)
	e.Enabled = enabled == 1
	e.Tags = splitTags(tags)
	return &e, nil
}

func (s *Store) CreateEntry(ctx context.Context, e *domain.Entry) error {
	if e.Rate == 0 {
		e.Rate = 1
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO entries (name, inbound_id, chain_id, display_host, display_port, rate, sort, enabled, tags, region, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Name, e.InboundID, nullInt64(e.ChainID), e.DisplayHost, e.DisplayPort, e.Rate, e.Sort, boolInt(e.Enabled), joinTags(e.Tags), strings.ToUpper(e.Region), ts, ts)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// EntryLine is an entry joined with the inbound it points at.
type EntryLine struct {
	Entry   domain.Entry
	Inbound domain.Inbound
}

// EntriesForUser returns enabled entries whose inbound the user may use:
// inbound group is NULL or equals the user's group.
func (s *Store) EntriesForUser(ctx context.Context, u *domain.User) ([]EntryLine, error) {
	return s.entriesForUser(ctx, u, false)
}

// EntriesForUserAll is EntriesForUser without the per-user blacklist: what
// the admin sees when deciding what to hide.
func (s *Store) EntriesForUserAll(ctx context.Context, u *domain.User) ([]EntryLine, error) {
	return s.entriesForUser(ctx, u, true)
}

func (s *Store) entriesForUser(ctx context.Context, u *domain.User, withBlocked bool) ([]EntryLine, error) {
	groups, err := s.AccessGroups(ctx, u, time.Now())
	if err != nil {
		return nil, err
	}
	clause, args := groupClause("i.group_id", groups)
	q := `SELECT ` + entryCols + `, ` + inboundColsPrefixed("i") + `
		FROM entries e JOIN inbounds i ON i.id = e.inbound_id
		WHERE e.enabled = 1 AND i.enabled = 1 AND ` + clause
	if !withBlocked {
		q += ` AND e.id NOT IN (SELECT entry_id FROM user_entry_blocks WHERE user_id = ?)`
		args = append(args, u.ID)
	}
	q += ` ORDER BY e.sort, e.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntryLine
	for rows.Next() {
		var l EntryLine
		var chain, group, ingress sql.NullInt64
		var eEnabled, iEnabled int
		var settings, tags string
		if err := rows.Scan(&l.Entry.ID, &l.Entry.Name, &l.Entry.InboundID, &chain, &l.Entry.DisplayHost, &l.Entry.DisplayPort, &l.Entry.Rate, &l.Entry.Sort, &eEnabled, &tags, &l.Entry.Region,
			&l.Inbound.ID, &l.Inbound.NodeID, &l.Inbound.Tag, &l.Inbound.Protocol, &l.Inbound.Listen, &l.Inbound.Port, &l.Inbound.Core, &settings, &group, &iEnabled, &l.Inbound.Sort, &ingress); err != nil {
			return nil, err
		}
		if err := unmarshalSettings(settings, &l.Inbound); err != nil {
			return nil, err
		}
		l.Entry.ChainID, l.Inbound.GroupID, l.Inbound.IngressID = int64Ptr(chain), int64Ptr(group), int64Ptr(ingress)
		l.Entry.Enabled, l.Inbound.Enabled = eEnabled == 1, iEnabled == 1
		l.Entry.Tags = splitTags(tags)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListEntries returns every entry.
func (s *Store) ListEntries(ctx context.Context) ([]*domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryCols+` FROM entries e ORDER BY e.sort, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEntry(ctx context.Context, e *domain.Entry) error {
	_, err := s.db.ExecContext(ctx, `UPDATE entries SET name = ?, inbound_id = ?, chain_id = ?, display_host = ?, display_port = ?, rate = ?, sort = ?, enabled = ?, tags = ?, region = ?, updated_at = ? WHERE id = ?`,
		e.Name, e.InboundID, nullInt64(e.ChainID), e.DisplayHost, e.DisplayPort, e.Rate, e.Sort, boolInt(e.Enabled), joinTags(e.Tags), strings.ToUpper(e.Region), now(), e.ID)
	return err
}

// ReorderEntries sets sort by position for the given ids (drag-and-drop).
func (s *Store) ReorderEntries(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE entries SET sort = ?, updated_at = ? WHERE id = ?`, (i+1)*10, now(), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EntryTags returns every distinct tag in use.
func (s *Store) EntryTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tags FROM entries WHERE tags != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, tag := range splitTags(t) {
			if !seen[tag] {
				seen[tag] = true
				out = append(out, tag)
			}
		}
	}
	sort.Strings(out)
	return out, rows.Err()
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func joinTags(tags []string) string {
	clean := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(strings.ReplaceAll(t, ",", " "))
		if t != "" && !seen[t] {
			seen[t] = true
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, ",")
}

func (s *Store) DeleteEntry(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	return err
}
