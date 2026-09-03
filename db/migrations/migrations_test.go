package migrations

import (
	"database/sql"
	"strings"
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

// TestTrimExistingPostingWhitespace_TrimsPaddedFields verifies the 00005
// migration's backfill: postings written before sync started trimming
// fetched fields (see sync.sanitizePosting) had padded whitespace on
// free-text columns, e.g. Stripe's Greenhouse listings showing up as
// both "Dublin" and "Dublin ". This migration cleans up what's already
// stored; raw_payload is deliberately left untouched (raw source JSON,
// kept verbatim for audit).
func TestTrimExistingPostingWhitespace_TrimsPaddedFields(t *testing.T) {
	sqlDB := migrateTo(t, 4)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (
			id, company_id, source, source_id, title, department, team, location,
			employment_type, workplace_type, description_html, description_text,
			job_url, application_url, raw_payload
		 ) VALUES (
			1, 1, 'ashby', ' job-1 ', ' Engineer ', ' Engineering', 'Core ', ' Dublin, Ireland ',
			' FullTime ', ' Remote ', ' <p>desc</p> ', ' desc ',
			' https://example.com/job-1 ', ' https://example.com/job-1/apply ', ' {"padded":true} '
		 )`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 5); err != nil {
		t.Fatalf("migrate to version 5: %v", err)
	}

	var (
		sourceID, title, department, team, location                     string
		employmentType, workplaceType, descriptionHTML, descriptionText string
		jobURL, applicationURL, rawPayload                              string
	)
	if err := sqlDB.QueryRow(
		`SELECT source_id, title, department, team, location,
		        employment_type, workplace_type, description_html, description_text,
		        job_url, application_url, raw_payload
		 FROM postings WHERE id = 1`,
	).Scan(
		&sourceID, &title, &department, &team, &location,
		&employmentType, &workplaceType, &descriptionHTML, &descriptionText,
		&jobURL, &applicationURL, &rawPayload,
	); err != nil {
		t.Fatalf("query posting: %v", err)
	}

	for name, got := range map[string]string{
		"source_id":        sourceID,
		"title":            title,
		"department":       department,
		"team":             team,
		"location":         location,
		"employment_type":  employmentType,
		"workplace_type":   workplaceType,
		"description_html": descriptionHTML,
		"description_text": descriptionText,
		"job_url":          jobURL,
		"application_url":  applicationURL,
	} {
		if got != strings.TrimSpace(got) || strings.Contains(got, "  ") {
			t.Errorf("%s = %q, want trimmed", name, got)
		}
	}
	if location != "Dublin, Ireland" {
		t.Errorf("location = %q, want %q", location, "Dublin, Ireland")
	}
	if rawPayload != ` {"padded":true} ` {
		t.Errorf("raw_payload = %q, want untouched", rawPayload)
	}
}

// TestPostingsOptionalFieldsNotNull_BackfillsExistingNullsToEmptyString
// verifies the 00006 migration's rebuild: an existing row with NULL in
// every optional TEXT column (the pre-migration schema's default for an
// omitted column) must come out as ” after migrating, not be dropped or
// left NULL.
func TestPostingsOptionalFieldsNotNull_BackfillsExistingNullsToEmptyString(t *testing.T) {
	sqlDB := migrateTo(t, 5)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting with every optional column omitted (NULL): %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 6); err != nil {
		t.Fatalf("migrate to version 6: %v", err)
	}

	var department, team, location, employmentType, workplaceType string
	var descriptionHTML, descriptionText, jobURL, applicationURL string
	if err := sqlDB.QueryRow(
		`SELECT department, team, location, employment_type, workplace_type,
		        description_html, description_text, job_url, application_url
		 FROM postings WHERE id = 1`,
	).Scan(
		&department, &team, &location, &employmentType, &workplaceType,
		&descriptionHTML, &descriptionText, &jobURL, &applicationURL,
	); err != nil {
		t.Fatalf("query posting (scanning into plain string, not sql.NullString, itself proves NOT NULL): %v", err)
	}

	for name, got := range map[string]string{
		"department": department, "team": team, "location": location,
		"employment_type": employmentType, "workplace_type": workplaceType,
		"description_html": descriptionHTML, "description_text": descriptionText,
		"job_url": jobURL, "application_url": applicationURL,
	} {
		if got != "" {
			t.Errorf("%s = %q, want empty string (backfilled from NULL)", name, got)
		}
	}
}

// TestPostingsOptionalFieldsNotNull_RejectsExplicitNull verifies the NOT
// NULL constraint is actually enforced after 00006, not just that existing
// NULLs got backfilled once.
func TestPostingsOptionalFieldsNotNull_RejectsExplicitNull(t *testing.T) {
	sqlDB := migrateTo(t, 6)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, department, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', NULL, '{}')`,
	); err == nil {
		t.Fatal("insert with explicit NULL department succeeded, want a NOT NULL constraint failure")
	}
}

// TestPostingsOptionalFieldsNotNull_OmittedColumnDefaultsToEmptyString
// verifies the DEFAULT ” half of the constraint: omitting an optional
// column entirely (as every real ingestion call site that doesn't have a
// value for it does) must populate ” via the column default, not fail.
func TestPostingsOptionalFieldsNotNull_OmittedColumnDefaultsToEmptyString(t *testing.T) {
	sqlDB := migrateTo(t, 6)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
		 VALUES (1, 1, 'ashby', 'job-1', 'Software Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting with department column omitted: %v", err)
	}

	var department string
	if err := sqlDB.QueryRow(`SELECT department FROM postings WHERE id = 1`).Scan(&department); err != nil {
		t.Fatalf("query department: %v", err)
	}
	if department != "" {
		t.Fatalf("department = %q, want empty string (column default)", department)
	}
}

// TestDocumentReviewsCheckConstraints_RejectInvalidValues verifies the
// 00007 migration's CHECK constraints are actually enforced at the DB
// level -- store.CreateDocumentReview only ever passes the known
// DocumentType*/ReviewOutcome* constants, so Go-level tests never
// exercise the rejection path; this pins it directly against the schema.
func TestDocumentReviewsCheckConstraints_RejectInvalidValues(t *testing.T) {
	sqlDB := migrateTo(t, 7)

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
		`INSERT INTO applications (id, posting_id, status) VALUES (1, 1, 'application_started')`,
	); err != nil {
		t.Fatalf("insert application: %v", err)
	}

	if _, err := sqlDB.Exec(
		`INSERT INTO document_reviews (application_id, document_type, cycle, content_snapshot, content_sha256, outcome)
		 VALUES (1, 'cv', 1, 'content', 'deadbeef', 'passed')`,
	); err == nil {
		t.Fatal("insert with document_type = 'cv' succeeded, want a CHECK constraint failure")
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO document_reviews (application_id, document_type, cycle, content_snapshot, content_sha256, outcome)
		 VALUES (1, 'cover_letter', 1, 'content', 'deadbeef', 'great')`,
	); err == nil {
		t.Fatal("insert with outcome = 'great' succeeded, want a CHECK constraint failure")
	}
}

// TestDocumentReviewsUniqueConstraint_RejectsDuplicateCycle verifies the
// UNIQUE(application_id, document_type, cycle) constraint -- the safety
// net behind store.CreateDocumentReview's own cycle computation, in case
// a future caller ever bypasses it.
func TestDocumentReviewsUniqueConstraint_RejectsDuplicateCycle(t *testing.T) {
	sqlDB := migrateTo(t, 7)

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
		`INSERT INTO applications (id, posting_id, status) VALUES (1, 1, 'application_started')`,
	); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO document_reviews (application_id, document_type, cycle, content_snapshot, content_sha256, outcome)
		 VALUES (1, 'cover_letter', 1, 'content', 'deadbeef', 'passed')`,
	); err != nil {
		t.Fatalf("insert first review: %v", err)
	}

	if _, err := sqlDB.Exec(
		`INSERT INTO document_reviews (application_id, document_type, cycle, content_snapshot, content_sha256, outcome)
		 VALUES (1, 'cover_letter', 1, 'other content', 'cafebabe', 'flagged')`,
	); err == nil {
		t.Fatal("insert with duplicate (application_id, document_type, cycle) succeeded, want a UNIQUE constraint failure")
	}
}
