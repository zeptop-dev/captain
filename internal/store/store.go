// Package store is the persistence layer: hand-written SQL, one file per
// aggregate. Every method takes a context and returns domain types.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("store: not found")

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// New returns a store over db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the handle for transactions in service code.
func (s *Store) DB() *sql.DB { return s.db }

func now() int64 { return time.Now().Unix() }

func unix(v int64) time.Time { return time.Unix(v, 0) }

func unixPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func nullTime(p *time.Time) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: p.Unix(), Valid: true}
}

func wrapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

var _ = context.Background
