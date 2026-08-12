// Package store is the hand-written repository layer wrapping the
// sqlc-generated code in store/db. It exposes clean domain types instead of
// raw database/sql nullability so callers don't need to know about sqlc or
// database/sql internals.
package store

import (
	"database/sql"

	"github.com/dklassen/swamp/store/db"
)

type Store struct {
	queries *db.Queries
}

func New(sqlDB *sql.DB) *Store {
	return &Store{queries: db.New(sqlDB)}
}
