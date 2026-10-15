// Package store is the hand-written repository layer wrapping the
// sqlc-generated code in store/sqlcdb. It exposes clean domain types instead of
// raw database/sql nullability so callers don't need to know about sqlc or
// database/sql internals.
package store

import (
	"database/sql"

	"github.com/dklassen/swamp/store/sqlcdb"
)

type Store struct {
	sqlDB   *sql.DB
	queries *sqlcdb.Queries
}

func New(sqlDB *sql.DB) *Store {
	return &Store{sqlDB: sqlDB, queries: sqlcdb.New(sqlDB)}
}
