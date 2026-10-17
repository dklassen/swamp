// Package store opens Swamp's database and runs statements on it: the
// connection settings, transactions, and the sqlc-generated queries in
// store/sqlcdb that the repositories use.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/store/sqlcdb"
)

// Store is the database. Statements run on it one at a time, each committed
// on its own.
type Store struct{ sqlDB *sql.DB }

// New is the Store on sqlDB, which Open made.
func New(sqlDB *sql.DB) Store { return Store{sqlDB: sqlDB} }

// Runner is a Store or a Tx: what a statement can run on. Only store's types
// implement it.
type Runner interface {
	queries() (*sqlcdb.Queries, error)
}

// Tx is a transaction (or a savepoint within one). Statements run on it
// commit together or not at all. Only InTx makes one.
type Tx struct{ tx *sql.Tx }

func (s Store) queries() (*sqlcdb.Queries, error) {
	if s.sqlDB == nil {
		return nil, errors.New("store: zero Store")
	}
	return sqlcdb.New(s.sqlDB), nil
}

func (tx Tx) queries() (*sqlcdb.Queries, error) {
	if tx.tx == nil {
		return nil, errors.New("store: zero Tx")
	}
	return sqlcdb.New(tx.tx), nil
}

// Write is Read for statements whose result the caller doesn't need.
func Write(ctx context.Context, r Runner, fn func(*sqlcdb.Queries) error) error {
	_, err := Read(ctx, r, func(q *sqlcdb.Queries) (struct{}, error) {
		return struct{}{}, fn(q)
	})
	return err
}

// InTx runs fn atomically: in a new transaction when r is a Store, or in a
// savepoint when r is a Tx. If fn returns an error, its writes are undone
// and the caller's are kept.
func InTx(ctx context.Context, r Runner, fn func(Tx) error) error {
	if _, err := r.queries(); err != nil {
		return err
	}
	switch r := r.(type) {
	case Store:
		return beginTx(ctx, r, fn)
	case Tx:
		return savepoint(ctx, r, fn)
	default:
		return fmt.Errorf("store: InTx on %T", r)
	}
}

// SQLite allows a savepoint name to repeat; ROLLBACK TO and RELEASE act on
// the most recent one, so nested savepoints can share it.
const savepointName = "store_intx"

func savepoint(ctx context.Context, tx Tx, fn func(Tx) error) error {
	if _, err := tx.tx.ExecContext(ctx, "SAVEPOINT "+savepointName); err != nil {
		return fmt.Errorf("store: savepoint: %w", err)
	}
	if err := fn(tx); err != nil {
		// ROLLBACK TO keeps the savepoint open, so it still needs releasing.
		if _, rbErr := tx.tx.ExecContext(ctx, "ROLLBACK TO "+savepointName); rbErr != nil {
			return errors.Join(err, fmt.Errorf("store: roll back to savepoint: %w", rbErr))
		}
		if _, relErr := tx.tx.ExecContext(ctx, "RELEASE "+savepointName); relErr != nil {
			return errors.Join(err, fmt.Errorf("store: release savepoint: %w", relErr))
		}
		return err
	}
	if _, err := tx.tx.ExecContext(ctx, "RELEASE "+savepointName); err != nil {
		return fmt.Errorf("store: release savepoint: %w", err)
	}
	return nil
}

func beginTx(ctx context.Context, s Store, fn func(Tx) error) error {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(Tx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// Read runs fn on r and maps sql.ErrNoRows to ErrNotFound.
func Read[T any](ctx context.Context, r Runner, fn func(*sqlcdb.Queries) (T, error)) (T, error) {
	var zero T
	q, err := r.queries()
	if err != nil {
		return zero, err
	}
	v, err := fn(q)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrNotFound
	}
	return v, err
}
