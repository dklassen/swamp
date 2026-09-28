package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Open opens the sqlite database at path, set up for more than one writer:
// the TUI, `swamp fetch`, and `swamp mcp-serve` all write the same file.
// busy_timeout makes a writer that finds the database locked wait up to
// 5s for it rather than failing with SQLITE_BUSY (see issue #136). WAL
// lets readers carry on while a write is in progress, so the TUI stays
// responsive during a sync; it's recorded in the file itself, and adds
// -wal/-shm files beside it.
//
// Transactions begin IMMEDIATE, taking the write lock at BeginTx rather
// than at their first write. UpsertPosting and CreateCompany read and then
// write in one transaction; begun deferred, another connection committing
// in between would make the write fail at once, which busy_timeout can't
// retry. Waiting at BeginTx is where busy_timeout applies.
func Open(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
}
