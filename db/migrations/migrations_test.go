package migrations

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"
)

func migrateTo(t *testing.T, version int64) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.UpTo(sqlDB, ".", version); err != nil {
		t.Fatalf("migrate to version %d: %v", version, err)
	}
	return sqlDB
}

// TestSplitApplicationFromPosting_MigratesAppliedMarkupIntoApplication
// verifies the 00002 migration's data copy: an 'applied' posting_markup
// row must produce a corresponding applications row (status
// 'application_submitted'), and posting_markup's own status must narrow
// down to 'interested' rather than being left pointing at a value the
// new, tighter CHECK constraint no longer allows.
func TestSplitApplicationFromPosting_MigratesAppliedMarkupIntoApplication(t *testing.T) {
	sqlDB := migrateTo(t, 1)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO posting_markup (posting_id, user_status) VALUES (1, 'applied')`,
	); err != nil {
		t.Fatalf("insert posting_markup: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 2); err != nil {
		t.Fatalf("migrate to version 2: %v", err)
	}

	var markupStatus string
	if err := sqlDB.QueryRow(`SELECT user_status FROM posting_markup WHERE posting_id = 1`).Scan(&markupStatus); err != nil {
		t.Fatalf("query posting_markup.user_status: %v", err)
	}
	if markupStatus != "interested" {
		t.Fatalf("posting_markup.user_status = %q, want %q", markupStatus, "interested")
	}

	var appStatus string
	if err := sqlDB.QueryRow(`SELECT status FROM applications WHERE posting_id = 1`).Scan(&appStatus); err != nil {
		t.Fatalf("query applications.status: %v", err)
	}
	if appStatus != "application_submitted" {
		t.Fatalf("applications.status = %q, want %q", appStatus, "application_submitted")
	}
}

// TestSplitApplicationFromPosting_RepointsInterviewStagesToApplication
// verifies interview_stages rows survive the migration re-pointed at the
// backfilled application rather than being dropped.
func TestSplitApplicationFromPosting_RepointsInterviewStagesToApplication(t *testing.T) {
	sqlDB := migrateTo(t, 1)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO posting_markup (posting_id, user_status) VALUES (1, 'interviewing')`,
	); err != nil {
		t.Fatalf("insert posting_markup: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO interview_stages (id, posting_id, sequence, name) VALUES (1, 1, 1, 'Recruiter Screen')`,
	); err != nil {
		t.Fatalf("insert interview_stage: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 2); err != nil {
		t.Fatalf("migrate to version 2: %v", err)
	}

	var wantApplicationID int64
	if err := sqlDB.QueryRow(`SELECT id FROM applications WHERE posting_id = 1`).Scan(&wantApplicationID); err != nil {
		t.Fatalf("query applications.id: %v", err)
	}

	var gotApplicationID int64
	if err := sqlDB.QueryRow(`SELECT application_id FROM interview_stages WHERE id = 1`).Scan(&gotApplicationID); err != nil {
		t.Fatalf("query interview_stages.application_id: %v", err)
	}
	if gotApplicationID != wantApplicationID {
		t.Fatalf("interview_stages.application_id = %d, want %d (the backfilled application's own id)", gotApplicationID, wantApplicationID)
	}
}

// TestPostingMarkupInterestedArchivedFlags_MigratesInterestedStatusToTimestamp
// verifies the 00003 migration's data copy: an existing 'interested'
// posting_markup row must produce a non-null interested_at (and null
// archived_at), not be silently dropped when user_status disappears.
func TestPostingMarkupInterestedArchivedFlags_MigratesInterestedStatusToTimestamp(t *testing.T) {
	sqlDB := migrateTo(t, 2)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO posting_markup (posting_id, user_status) VALUES (1, 'interested')`,
	); err != nil {
		t.Fatalf("insert posting_markup: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 3); err != nil {
		t.Fatalf("migrate to version 3: %v", err)
	}

	var interestedAt sql.NullTime
	var archivedAt sql.NullTime
	if err := sqlDB.QueryRow(
		`SELECT interested_at, archived_at FROM posting_markup WHERE posting_id = 1`,
	).Scan(&interestedAt, &archivedAt); err != nil {
		t.Fatalf("query posting_markup: %v", err)
	}
	if !interestedAt.Valid {
		t.Fatal("interested_at is NULL, want non-null (migrated from user_status='interested')")
	}
	if archivedAt.Valid {
		t.Fatal("archived_at is non-null, want NULL")
	}
}

// TestDropApplicationStatusCheckConstraint_PreservesExistingApplicationRow
// verifies the 00004 migration's rebuild of applications (dropping its
// status CHECK constraint -- validation moved to Go, see
// store.ParseApplicationStatus and decisions.log) doesn't lose or alter
// data already in the table.
func TestDropApplicationStatusCheckConstraint_PreservesExistingApplicationRow(t *testing.T) {
	sqlDB := migrateTo(t, 3)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO applications (posting_id, status, notes) VALUES (1, 'interviewing', 'great chat')`,
	); err != nil {
		t.Fatalf("insert application: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 4); err != nil {
		t.Fatalf("migrate to version 4: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 4 {
		t.Fatalf("DB version after UpTo(4) = %d, want 4 (migration 00004 not found?)", gotVersion)
	}

	var status, notes string
	if err := sqlDB.QueryRow(`SELECT status, notes FROM applications WHERE posting_id = 1`).Scan(&status, &notes); err != nil {
		t.Fatalf("query applications: %v", err)
	}
	if status != "interviewing" {
		t.Fatalf("applications.status = %q, want %q", status, "interviewing")
	}
	if notes != "great chat" {
		t.Fatalf("applications.notes = %q, want %q", notes, "great chat")
	}
}

// TestDropApplicationStatusCheckConstraint_ArbitraryStatusValueAccepted
// verifies the CHECK constraint on applications.status is actually gone
// after 00004: a value outside the old fixed set, which would have failed
// under 00002's CHECK, must now insert cleanly.
func TestDropApplicationStatusCheckConstraint_ArbitraryStatusValueAccepted(t *testing.T) {
	sqlDB := migrateTo(t, 4)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO applications (posting_id, status) VALUES (1, 'anything-goes')`,
	); err != nil {
		t.Fatalf("insert application with arbitrary status = %v, want success (CHECK constraint dropped in 00004)", err)
	}
}

// TestApplicationStatusHasNoDBDefault verifies status has neither a NOT
// NULL constraint nor a DEFAULT after 00004: omitting it from an INSERT
// must leave it NULL, not silently populate 'application_started' -- the
// DB no longer decides the initial value, the application does (see PR
// #17 review, decisions.log).
func TestApplicationStatusHasNoDBDefault(t *testing.T) {
	sqlDB := migrateTo(t, 4)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO applications (posting_id) VALUES (1)`); err != nil {
		t.Fatalf("insert application without status = %v, want success (column is nullable, no default)", err)
	}

	var status sql.NullString
	if err := sqlDB.QueryRow(`SELECT status FROM applications WHERE posting_id = 1`).Scan(&status); err != nil {
		t.Fatalf("query applications.status: %v", err)
	}
	if status.Valid {
		t.Fatalf("applications.status = %q, want NULL (no DB default)", status.String)
	}
}
