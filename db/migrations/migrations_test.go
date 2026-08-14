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
