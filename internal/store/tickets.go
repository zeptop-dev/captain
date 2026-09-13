package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

// Ticket statuses.
const (
	TicketOpen    = "open"    // waiting for the operator
	TicketReplied = "replied" // operator answered, waiting for the user
	TicketClosed  = "closed"
)

// TicketRow is a ticket with its owner's email and message count.
type TicketRow struct {
	domain.Ticket
	Email    string
	Messages int
}

// CreateTicket opens a ticket with its first message.
func (s *Store) CreateTicket(ctx context.Context, userID int64, subject, priority, body string) (*domain.Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO tickets (user_id, subject, status, priority, created_at, updated_at) VALUES (?, ?, 'open', ?, ?, ?)`, userID, subject, priority, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_messages (ticket_id, from_admin, body, created_at) VALUES (?, 0, ?, ?)`, id, body, ts); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.TicketByID(ctx, id)
}

// TicketByID loads a ticket; userID > 0 restricts it to that owner.
func (s *Store) TicketByID(ctx context.Context, id int64) (*domain.Ticket, error) {
	var t domain.Ticket
	var created, updated int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, subject, status, priority, created_at, updated_at FROM tickets WHERE id = ?`, id).
		Scan(&t.ID, &t.UserID, &t.Subject, &t.Status, &t.Priority, &created, &updated)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	t.CreatedAt, t.UpdatedAt = unix(created), unix(updated)
	return &t, nil
}

// TicketMessages lists a ticket's messages oldest first.
func (s *Store) TicketMessages(ctx context.Context, ticketID int64) ([]domain.TicketMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ticket_id, from_admin, body, created_at FROM ticket_messages WHERE ticket_id = ? ORDER BY id`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TicketMessage
	for rows.Next() {
		var m domain.TicketMessage
		var admin int
		var created int64
		if err := rows.Scan(&m.ID, &m.TicketID, &admin, &m.Body, &created); err != nil {
			return nil, err
		}
		m.FromAdmin, m.CreatedAt = admin == 1, unix(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplyTicket appends a message; from the operator the status becomes
// replied, from the user it goes back to open.
func (s *Store) ReplyTicket(ctx context.Context, ticketID int64, fromAdmin bool, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_messages (ticket_id, from_admin, body, created_at) VALUES (?, ?, ?, ?)`, ticketID, boolInt(fromAdmin), body, ts); err != nil {
		return err
	}
	status := TicketOpen
	if fromAdmin {
		status = TicketReplied
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`, status, ts, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetTicketStatus closes or reopens a ticket.
func (s *Store) SetTicketStatus(ctx context.Context, ticketID int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`, status, now(), ticketID)
	return err
}

// ListTickets returns tickets newest-updated first; userID 0 = all users,
// status "" = any.
func (s *Store) ListTickets(ctx context.Context, userID int64, status string, limit, offset int) ([]TicketRow, int, error) {
	where, args := "WHERE 1=1", []any{}
	if userID > 0 {
		where, args = where+" AND t.user_id = ?", append(args, userID)
	}
	if status != "" {
		where, args = where+" AND t.status = ?", append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets t `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.user_id, t.subject, t.status, t.priority, t.created_at, t.updated_at, u.email,
		(SELECT COUNT(*) FROM ticket_messages m WHERE m.ticket_id = t.id)
		FROM tickets t JOIN users u ON u.id = t.user_id `+where+` ORDER BY t.updated_at DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []TicketRow
	for rows.Next() {
		var t TicketRow
		var created, updated int64
		if err := rows.Scan(&t.ID, &t.UserID, &t.Subject, &t.Status, &t.Priority, &created, &updated, &t.Email, &t.Messages); err != nil {
			return nil, 0, err
		}
		t.CreatedAt, t.UpdatedAt = unix(created), unix(updated)
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// OpenTickets counts tickets waiting for the operator.
func (s *Store) OpenTickets(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets WHERE status = 'open'`).Scan(&n)
	return n, err
}

// ---- articles (knowledge base) ----------------------------------------------

func (s *Store) CreateArticle(ctx context.Context, a *domain.Article) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO articles (title, category, body, lang, sort, published, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Title, a.Category, a.Body, a.Lang, a.Sort, boolInt(a.Published), ts, ts)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateArticle(ctx context.Context, a *domain.Article) error {
	_, err := s.db.ExecContext(ctx, `UPDATE articles SET title = ?, category = ?, body = ?, lang = ?, sort = ?, published = ?, updated_at = ? WHERE id = ?`,
		a.Title, a.Category, a.Body, a.Lang, a.Sort, boolInt(a.Published), now(), a.ID)
	return err
}

func (s *Store) DeleteArticle(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM articles WHERE id = ?`, id)
	return err
}

func (s *Store) ArticleByID(ctx context.Context, id int64) (*domain.Article, error) {
	return scanArticle(s.db.QueryRowContext(ctx, `SELECT id, title, category, body, lang, sort, published, created_at, updated_at FROM articles WHERE id = ?`, id))
}

// ListArticles returns articles by sort then title; publishedOnly hides drafts.
func (s *Store) ListArticles(ctx context.Context, publishedOnly bool) ([]*domain.Article, error) {
	q := `SELECT id, title, category, body, lang, sort, published, created_at, updated_at FROM articles`
	if publishedOnly {
		q += ` WHERE published = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY category, sort, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanArticle(row interface{ Scan(...any) error }) (*domain.Article, error) {
	var a domain.Article
	var pub int
	var created, updated int64
	if err := row.Scan(&a.ID, &a.Title, &a.Category, &a.Body, &a.Lang, &a.Sort, &pub, &created, &updated); err != nil {
		return nil, wrapNotFound(err)
	}
	a.Published, a.CreatedAt, a.UpdatedAt = pub == 1, unix(created), unix(updated)
	return &a, nil
}

// ---- telegram ------------------------------------------------------------------

// NewTelegramBindCode issues a short-lived code the user sends to the bot.
func (s *Store) NewTelegramBindCode(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	code := randomCode(8)
	_, err := s.db.ExecContext(ctx, `INSERT INTO telegram_bind_codes (code, user_id, expires_at) VALUES (?, ?, ?)`, code, userID, time.Now().Add(ttl).Unix())
	return code, err
}

// BindTelegram consumes a bind code and links the chat to its user.
func (s *Store) BindTelegram(ctx context.Context, code string, chatID int64) (*domain.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var userID int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM telegram_bind_codes WHERE code = ? AND expires_at > ?`, code, now()).Scan(&userID); err != nil {
		return nil, wrapNotFound(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET telegram_id = NULL WHERE telegram_id = ?`, chatID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET telegram_id = ?, updated_at = ? WHERE id = ?`, chatID, now(), userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM telegram_bind_codes WHERE user_id = ? OR expires_at <= ?`, userID, now()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.UserByID(ctx, userID)
}

// UnbindTelegram clears the link.
func (s *Store) UnbindTelegram(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET telegram_id = NULL, updated_at = ? WHERE id = ?`, now(), userID)
	return err
}

// TelegramID returns the chat linked to a user, 0 when none.
func (s *Store) TelegramID(ctx context.Context, userID int64) (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = ?`, userID).Scan(&id); err != nil {
		return 0, wrapNotFound(err)
	}
	return id.Int64, nil
}

// UserByTelegramID finds the user linked to a chat.
func (s *Store) UserByTelegramID(ctx context.Context, chatID int64) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE telegram_id = ?`, chatID))
}
