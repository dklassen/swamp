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

	// TimeFormat names the format the driver writes a time.Time in (its
	// _time_format DSN parameter): "sqlite" for
	// "2006-01-02 15:04:05.999999999-07:00", or "datetime" for
	// "2006-01-02 15:04:05". Leave it at DefaultConfig's "sqlite": empty
	// falls back to the driver's time.Time.String(), which can't be read
	// back for a zone Go left unnamed, and saving a posting fails (issue
	// #140). The driver rejects any other value when it connects.
	TimeFormat string
}

// DefaultConfig is the Config swamp runs with unless told otherwise.
//
// TimeFormat "sqlite" writes "2006-01-02 15:04:05.999999999-07:00"
// instead of the driver's default, time.Time.String(). String() names the
// zone only when the offset matches the machine's local timezone, so a
// Greenhouse "-04:00" parsed on a UTC machine was stored as "-0400 -0400",
// which the driver can't read back: every save of such a posting failed
// (issue #140). A numeric offset reads back the same wherever it was
// written. Rows already stored in String() format still read back, so no
// migration is needed.
func DefaultConfig() Config {
	return Config{BusyTimeout: 5 * time.Second, TimeFormat: "sqlite"}
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
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(%s)&_txlock=%s&_time_format=%s",
		path, cfg.BusyTimeout.Milliseconds(), journalMode, txLock, cfg.TimeFormat)
	return sql.Open("sqlite", dsn)
}
