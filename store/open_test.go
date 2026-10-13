package store

import (
	"context"
	"database/sql"
	"strings"
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

// TestOpen_WritesTimesInTheConfiguredFormat checks Open writes a
// time.Time in the format cfg.TimeFormat names. The default must keep the
// numeric offset: the driver's own default, time.Time.String(), can't be
// read back for a zone Go left unnamed -- see issue #140.
func TestOpen_WritesTimesInTheConfiguredFormat(t *testing.T) {
	t.Parallel()

	written := time.Date(2026, 8, 24, 13, 15, 0, 0, time.FixedZone("", -4*60*60))
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "default, converted to UTC", cfg: DefaultConfig(), want: "2026-08-24 17:15:00+00:00"},
		{name: "sqlite format, no timezone", cfg: Config{BusyTimeout: time.Second, TimeFormat: "sqlite"}, want: "2026-08-24 13:15:00-04:00"},
		{name: "datetime", cfg: Config{BusyTimeout: time.Second, TimeFormat: "datetime"}, want: "2026-08-24 13:15:00"},
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
			if _, err := sqlDB.Exec("CREATE TABLE t (v TIMESTAMP)"); err != nil {
				t.Fatalf("create table: %v", err)
			}
			if _, err := sqlDB.Exec("INSERT INTO t (v) VALUES (?)", written); err != nil {
				t.Fatalf("insert: %v", err)
			}

			var got string
			if err := sqlDB.QueryRow("SELECT CAST(v AS TEXT) FROM t").Scan(&got); err != nil {
				t.Fatalf("select: %v", err)
			}
			if got != tt.want {
				t.Errorf("stored %q, want %q", got, tt.want)
			}
		})
	}
}

// TestOpen_EqualFold_AgreesWithGo: SQL that matches values ignoring case
// must agree with filter.Match, which uses strings.EqualFold; SQLite's
// NOCASE and lower() fold ASCII only.
func TestOpen_EqualFold_AgreesWithGo(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{"Engineering", "engineering"},
		{"Engineering", "Engineer"},
		{"Zürich", "zürich"},
		{"Zürich", "ZÜRICH"},
		{"Montréal", "MONTRÉAL"},
		{"Kraków", "Krakow"},
		{"", ""},
	}
	for _, origin := range []string{"", "tui:1"} {
		t.Run("origin "+origin, func(t *testing.T) {
			t.Parallel()

			path := t.TempDir() + "/test.db"
			newTestStoreAt(t, path) // migrate, so the origin can be stamped
			cfg := DefaultConfig()
			cfg.Origin = origin
			sqlDB, err := Open(path, cfg)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() {
				if err := sqlDB.Close(); err != nil {
					t.Errorf("close db: %v", err)
				}
			})

			for _, p := range pairs {
				var got bool
				if err := sqlDB.QueryRow("SELECT equal_fold(?, ?)", p[0], p[1]).Scan(&got); err != nil {
					t.Fatalf("equal_fold(%q, %q): %v", p[0], p[1], err)
				}
				if want := strings.EqualFold(p[0], p[1]); got != want {
					t.Errorf("equal_fold(%q, %q) = %v, want %v as strings.EqualFold", p[0], p[1], got, want)
				}
			}
		})
	}
}

// TestOpen_ContainsFold_AgreesWithGo: posting search matches text the way
// the TUI's lists do, with strings.Contains on lowered strings; SQLite's
// LIKE folds ASCII only and gives % and _ meaning.
func TestOpen_ContainsFold_AgreesWithGo(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{"Senior Platform Engineer", "platform"},
		{"Senior Platform Engineer", "PLATFORM ENG"},
		{"Platform Engineer", "backend"},
		{"ZÜRICH", "zürich"},
		{"Montréal", "MONTRÉ"},
		{"100% remote", "100%"},
		{"Engineer", "eng_neer"},
		{"Engineer", "e%r"},
		{"Engineer", ""},
		{"", "eng"},
	}
	for _, origin := range []string{"", "tui:1"} {
		t.Run("origin "+origin, func(t *testing.T) {
			t.Parallel()

			path := t.TempDir() + "/test.db"
			newTestStoreAt(t, path) // migrate, so the origin can be stamped
			cfg := DefaultConfig()
			cfg.Origin = origin
			sqlDB, err := Open(path, cfg)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() {
				if err := sqlDB.Close(); err != nil {
					t.Errorf("close db: %v", err)
				}
			})

			for _, p := range pairs {
				var got bool
				if err := sqlDB.QueryRow("SELECT contains_fold(?, ?)", p[0], p[1]).Scan(&got); err != nil {
					t.Fatalf("contains_fold(%q, %q): %v", p[0], p[1], err)
				}
				if want := strings.Contains(strings.ToLower(p[0]), strings.ToLower(p[1])); got != want {
					t.Errorf("contains_fold(%q, %q) = %v, want %v", p[0], p[1], got, want)
				}
			}
		})
	}
}
