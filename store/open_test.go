package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestOpen_TransactionsTakeTheWriteLockWhenTheyBegin checks a transaction
// holds the write lock from BeginTx, before its first statement. UpsertPosting
// and CreateCompany read and then write inside one transaction; begun
// deferred (sqlite's default), another connection could commit between
// the read and the write, and the write would then fail with SQLITE_BUSY
// straight away -- busy_timeout can't retry a stale snapshot. Taking the
// lock at BeginTx is where busy_timeout does apply (see issue #136).
func TestOpen_TransactionsTakeTheWriteLockWhenTheyBegin(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	sqlDB, err := Open(path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if _, err := sqlDB.Exec("CREATE TABLE t (v INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	// A second connection that doesn't wait for locks: if the transaction
	// above already holds the write lock, this write is refused at once.
	other, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open second connection: %v", err)
	}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Errorf("close second connection: %v", err)
		}
	})
	if _, err := other.Exec("INSERT INTO t (v) VALUES (1)"); err == nil {
		t.Error("write from another connection succeeded while a transaction was open, want it refused: the transaction didn't take the write lock at BeginTx")
	}
}

// TestOpen_ConfiguresConnectionForConcurrentWriters checks the settings
// Open applies so a second writer (the TUI, `swamp fetch`, and `swamp
// mcp-serve` all write the same file) waits for the lock rather than
// failing with SQLITE_BUSY -- see issue #136.
func TestOpen_ConfiguresConnectionForConcurrentWriters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    Config
		pragma string
		want   string
	}{
		{name: "default busy timeout", cfg: DefaultConfig(), pragma: "busy_timeout", want: "5000"},
		{name: "configured busy timeout", cfg: Config{BusyTimeout: 250 * time.Millisecond}, pragma: "busy_timeout", want: "250"},
		{name: "journal mode", cfg: DefaultConfig(), pragma: "journal_mode", want: "wal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sqlDB, err := Open(t.TempDir()+"/test.db", tt.cfg)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() {
				if err := sqlDB.Close(); err != nil {
					t.Errorf("close db: %v", err)
				}
			})

			var got string
			if err := sqlDB.QueryRow("PRAGMA " + tt.pragma).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s: %v", tt.pragma, err)
			}
			if got != tt.want {
				t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
}
