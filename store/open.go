package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds the tunable sqlite connection settings Open applies.
type Config struct {
	// BusyTimeout is how long a connection waits for a lock held by
	// another writer before failing with SQLITE_BUSY.
	BusyTimeout time.Duration
}

// DefaultConfig is the Config swamp runs with unless told otherwise.
func DefaultConfig() Config {
	return Config{BusyTimeout: 5 * time.Second}
}

// journalMode and txLock are what make more than one writer safe, so
// they're fixed rather than part of Config -- a different value would
// quietly bring back the SQLITE_BUSY failures in issue #136.
//
// WAL lets readers carry on while a write is in progress, so the TUI stays
// responsive during a sync. It's recorded in the database file itself, and
// adds -wal/-shm files beside it.
//
// Transactions begin IMMEDIATE, taking the write lock at BeginTx rather
// than at their first write. UpsertPosting and CreateCompany read and then
// write in one transaction; begun deferred, another connection committing
// in between would make the write fail at once, which busy_timeout can't
// retry. Waiting at BeginTx is where busy_timeout applies.
const (
	journalMode = "WAL"
	txLock      = "immediate"
)

// Open opens the sqlite database at path, set up for more than one writer:
// the TUI, `swamp fetch`, and `swamp mcp-serve` all write the same file.
// A writer that finds the database locked waits up to cfg.BusyTimeout for
// it rather than failing with SQLITE_BUSY (see issue #136).
func Open(path string, cfg Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(%s)&_txlock=%s",
		path, cfg.BusyTimeout.Milliseconds(), journalMode, txLock)
	return sql.Open("sqlite", dsn)
}
