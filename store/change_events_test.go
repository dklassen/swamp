package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// openWithOrigin needs path already migrated, as `swamp migrate up` guarantees.
func openWithOrigin(t *testing.T, path, origin string) *Store {
	t.Helper()
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
	return New(sqlDB)
}

func changeOrigins(t *testing.T, s *Store, table string) []string {
	t.Helper()
	rows, err := s.sqlDB.Query(`SELECT origin FROM change_events WHERE table_name = ? ORDER BY id`, table)
	if err != nil {
		t.Fatalf("query change_events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var origins []string
	for rows.Next() {
		var origin sql.NullString
		if err := rows.Scan(&origin); err != nil {
			t.Fatalf("scan: %v", err)
		}
		origins = append(origins, origin.String)
	}
	return origins
}

// TestChangeEvents_SwampConnectionsStampTheirOrigin: a writer without an
// origin stands in for the sqlite3 CLI, which must still be able to write.
func TestChangeEvents_SwampConnectionsStampTheirOrigin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	outsider := newTestStoreAt(t, path) // migrates; no Origin
	tui := openWithOrigin(t, path, "tui:1")
	acme := mustCreateCompany(t, outsider, "Acme", "ashby", "acme")
	first := mustUpsertPosting(t, outsider, acme.ID, "job-1", "Engineer")
	second := mustUpsertPosting(t, outsider, acme.ID, "job-2", "Designer")

	if _, err := tui.createApplication(ctx, first.ID); err != nil {
		t.Fatalf("CreateApplication through the stamped store: %v", err)
	}
	if _, err := outsider.createApplication(ctx, second.ID); err != nil {
		t.Fatalf("CreateApplication through the outsider: %v", err)
	}

	got := changeOrigins(t, outsider, "applications")
	if len(got) != 2 || got[0] != "tui:1" || got[1] != "" {
		t.Errorf("application change origins = %q, want [tui:1, \"\"]", got)
	}
}

// TestOpen_WithOrigin_KeepsTheConnectionSettings: stamping a connection
// must keep every setting Open sets for concurrent writers and times.
func TestOpen_WithOrigin_KeepsTheConnectionSettings(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path) // migrate
	s := openWithOrigin(t, path, "tui:1")

	for pragma, want := range map[string]string{"busy_timeout": "5000", "journal_mode": "wal"} {
		var got string
		if err := s.sqlDB.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}

	if _, err := s.sqlDB.Exec("CREATE TABLE t (v TIMESTAMP)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := s.sqlDB.Exec("INSERT INTO t (v) VALUES (?)", time.Date(2026, 8, 24, 13, 15, 0, 0, time.FixedZone("", -4*60*60))); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var stored string
	if err := s.sqlDB.QueryRow("SELECT CAST(v AS TEXT) FROM t").Scan(&stored); err != nil {
		t.Fatalf("select: %v", err)
	}
	if want := "2026-08-24 17:15:00+00:00"; stored != want {
		t.Errorf("stored %q, want %q", stored, want)
	}

	tx, err := s.sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	other, err := sql.Open("sqlite", "file:"+path) // doesn't wait for locks
	if err != nil {
		t.Fatalf("open second connection: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if _, err := other.Exec("INSERT INTO t (v) VALUES (NULL)"); err == nil {
		t.Error("another connection wrote while a transaction was open: the transaction didn't take the write lock at BeginTx")
	}
}

func TestOpen_WithOrigin_OnAnUnmigratedDatabase_SaysToMigrate(t *testing.T) {
	t.Parallel()

	s := openWithOrigin(t, t.TempDir()+"/test.db", "tui:1")

	err := s.sqlDB.Ping()
	if err == nil || !strings.Contains(err.Error(), "swamp migrate up") {
		t.Errorf("first use of a stamped store on an unmigrated database = %v, want an error saying to run swamp migrate up", err)
	}
}

func TestChangeEvents_RolledBackChangeLogsNothing(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path)
	s := openWithOrigin(t, path, "tui:1")
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")

	tx, err := s.sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO applications (posting_id, status) VALUES (?, 'application_started')`, posting.ID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if got := changeOrigins(t, s, "applications"); len(got) != 0 {
		t.Errorf("%d change events after a rolled-back insert, want 0", len(got))
	}
}

// TestChangeEvents_PostingMarkupLogsItsMarks: another TUI window shows a
// posting marked interested or archived, so those marks are logged; notes
// aren't.
func TestChangeEvents_PostingMarkupLogsItsMarks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	if _, err := s.setPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	if _, err := s.updatePostingMarkupNotes(ctx, posting.ID, "ask about on-call"); err != nil {
		t.Fatalf("UpdatePostingMarkupNotes: %v", err)
	}

	rows, err := s.sqlDB.Query(`SELECT op, row_id FROM change_events WHERE table_name = 'posting_markup' ORDER BY id`)
	if err != nil {
		t.Fatalf("query change_events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var op string
		var rowID int64
		if err := rows.Scan(&op, &rowID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if rowID != posting.ID {
			t.Errorf("%s event names row %d, want the posting's ID %d", op, rowID, posting.ID)
		}
		got = append(got, op)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if diff := cmp.Diff([]string{"insert", "update"}, got); diff != "" {
		t.Errorf("posting_markup events (-want +got), creating the posting, marking it interested, then changing only its notes:\n%s", diff)
	}
}
