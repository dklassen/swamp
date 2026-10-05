package migrations

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
// free-text columns, e.g. Initech's Greenhouse listings showing up as
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
// documents.Type/ReviewOutcome* constants, so Go-level tests never
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

// TestDropDocumentReviewsCheckConstraints_PreservesExistingRow verifies
// 00008's table-rebuild copies existing rows over intact -- same shape as
// TestDropApplicationStatusCheckConstraint_PreservesExistingApplicationRow
// above, for the analogous document_reviews rebuild.
func TestDropDocumentReviewsCheckConstraints_PreservesExistingRow(t *testing.T) {
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
		`INSERT INTO document_reviews (application_id, document_type, cycle, content_snapshot, content_sha256, outcome, notes)
		 VALUES (1, 'cover_letter', 1, 'content', 'deadbeef', 'flagged', 'too generic')`,
	); err != nil {
		t.Fatalf("insert document_review: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 8); err != nil {
		t.Fatalf("migrate to version 8: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 8 {
		t.Fatalf("DB version after UpTo(8) = %d, want 8 (migration 00008 not found?)", gotVersion)
	}

	var documentType, outcome, notes string
	if err := sqlDB.QueryRow(`SELECT document_type, outcome, notes FROM document_reviews WHERE application_id = 1`).Scan(&documentType, &outcome, &notes); err != nil {
		t.Fatalf("query document_reviews: %v", err)
	}
	if documentType != "cover_letter" || outcome != "flagged" || notes != "too generic" {
		t.Fatalf("document_reviews row = (%q, %q, %q), want (cover_letter, flagged, too generic)", documentType, outcome, notes)
	}
}

// TestDropDocumentReviewsCheckConstraints_ArbitraryValuesAccepted verifies
// the CHECK constraints on document_reviews.document_type/outcome are
// actually gone after 00008: values outside the old fixed sets, which
// would have failed under 00007's CHECK (see
// TestDocumentReviewsCheckConstraints_RejectInvalidValues), must now
// insert cleanly -- same shape as
// TestDropApplicationStatusCheckConstraint_ArbitraryStatusValueAccepted.
func TestDropDocumentReviewsCheckConstraints_ArbitraryValuesAccepted(t *testing.T) {
	sqlDB := migrateTo(t, 8)

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
		 VALUES (1, 'cv', 1, 'content', 'deadbeef', 'great')`,
	); err != nil {
		t.Fatalf("insert with document_type = 'cv', outcome = 'great' = %v, want success (CHECK constraints dropped in 00008)", err)
	}
}

// TestDropDocumentReviewsCheckConstraints_UniqueConstraintStillEnforced
// is a regression check that the 00008 table rebuild didn't lose the
// UNIQUE(application_id, document_type, cycle) constraint along with the
// CHECK constraints -- see
// TestDocumentReviewsUniqueConstraint_RejectsDuplicateCycle for the same
// check pinned to the pre-rebuild version.
func TestDropDocumentReviewsCheckConstraints_UniqueConstraintStillEnforced(t *testing.T) {
	sqlDB := migrateTo(t, 8)

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

// TestCloseApplicationsForClosedPostings_BacksFillsEarlyStagesOnly covers
// the one-time backfill for #105. The syncer only closes an application
// on the open->closed transition, so postings that were already closed
// before that logic existed would never be caught -- this migration
// catches them once, under exactly the same early-status rule the syncer
// applies from then on.
func TestCloseApplicationsForClosedPostings_BacksFillsEarlyStagesOnly(t *testing.T) {
	sqlDB := migrateTo(t, 8)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	for id, listingStatus := range map[int]string{1: "closed", 2: "closed", 3: "closed", 4: "closed", 5: "open"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload, listing_status)
			 VALUES (?, 1, 'ashby', ?, 'Engineer', '{}', ?)`,
			id, fmt.Sprintf("job-%d", id), listingStatus,
		); err != nil {
			t.Fatalf("insert posting %d: %v", id, err)
		}
	}

	// posting 1/2 closed + early status -> backfilled.
	// posting 3 closed but mid-process -> left alone.
	// posting 4 closed and already terminal -> left alone.
	// posting 5 still open -> left alone.
	for postingID, status := range map[int]string{
		1: "application_started",
		2: "application_submitted",
		3: "interviewing",
		4: "rejected",
		5: "application_started",
	} {
		if _, err := sqlDB.Exec(
			`INSERT INTO applications (posting_id, status) VALUES (?, ?)`, postingID, status,
		); err != nil {
			t.Fatalf("insert application for posting %d: %v", postingID, err)
		}
	}

	if err := goose.UpTo(sqlDB, ".", 9); err != nil {
		t.Fatalf("migrate to version 9: %v", err)
	}

	want := map[int]string{
		1: "posting_closed",
		2: "posting_closed",
		3: "interviewing",
		4: "rejected",
		5: "application_started",
	}
	for postingID, wantStatus := range want {
		var got string
		if err := sqlDB.QueryRow(`SELECT status FROM applications WHERE posting_id = ?`, postingID).Scan(&got); err != nil {
			t.Fatalf("query application for posting %d: %v", postingID, err)
		}
		if got != wantStatus {
			t.Errorf("application for posting %d has status %q, want %q", postingID, got, wantStatus)
		}
	}
}

// TestCompanyDescription_ExistingCompaniesGetEmptyDescription verifies the
// 00010 migration adds companies.description without disturbing existing
// rows, which read back as an empty string rather than NULL (see #74 for
// why optional TEXT columns are NOT NULL with an empty default).
func TestCompanyDescription_ExistingCompaniesGetEmptyDescription(t *testing.T) {
	sqlDB := migrateTo(t, 9)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 10); err != nil {
		t.Fatalf("migrate to version 10: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 10 {
		t.Fatalf("DB version after UpTo(10) = %d, want 10 (migration 00010 not found?)", gotVersion)
	}

	var name, description string
	if err := sqlDB.QueryRow(`SELECT name, description FROM companies WHERE id = 1`).Scan(&name, &description); err != nil {
		t.Fatalf("query companies: %v", err)
	}
	if name != "Acme" {
		t.Fatalf("companies.name = %q, want %q", name, "Acme")
	}
	if description != "" {
		t.Fatalf("companies.description = %q, want empty", description)
	}
}

// TestCompanyLastFetchedAt_ExistingCompaniesStartNeverFetched verifies the
// 00011 migration adds companies.last_fetched_at as NULL for existing rows:
// nothing recorded when they were last fetched, so they read as never
// fetched until their next sync.
func TestCompanyLastFetchedAt_ExistingCompaniesStartNeverFetched(t *testing.T) {
	sqlDB := migrateTo(t, 10)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 11); err != nil {
		t.Fatalf("migrate to version 11: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 11 {
		t.Fatalf("DB version after UpTo(11) = %d, want 11 (migration 00011 not found?)", gotVersion)
	}

	var lastFetched sql.NullTime
	if err := sqlDB.QueryRow(`SELECT last_fetched_at FROM companies WHERE id = 1`).Scan(&lastFetched); err != nil {
		t.Fatalf("query companies: %v", err)
	}
	if lastFetched.Valid {
		t.Fatalf("companies.last_fetched_at = %v, want NULL", lastFetched.Time)
	}
}

// TestPublishedAtUTC_ConvertsStoredTimesToUTC verifies the 00012 migration
// rewrites published_at values stored in the driver's old
// time.Time.String() format ("2006-01-02 15:04:05.999999999 -0700 MST")
// as the same instant in UTC, in the format store.Open now writes
// ("2006-01-02 15:04:05.999999999-07:00" in UTC). Inputs cover every
// shape found in the real database, plus crossing midnight and a
// positive, non-hour offset. NULL and values already in the new format
// are left alone (see issue #140).
func TestPublishedAtUTC_ConvertsStoredTimesToUTC(t *testing.T) {
	sqlDB := migrateTo(t, 11)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'greenhouse', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	tests := []struct {
		name   string
		stored sql.NullString
		want   sql.NullString
	}{
		{name: "named zone", stored: validString("2026-08-17 05:18:42 -0400 EDT"), want: validString("2026-08-17 09:18:42+00:00")},
		{name: "unnamed UTC, milliseconds", stored: validString("2026-02-14 17:27:16.004 +0000 +0000"), want: validString("2026-02-14 17:27:16.004+00:00")},
		{name: "unnamed UTC, two fraction digits", stored: validString("2026-05-12 16:12:44.74 +0000 +0000"), want: validString("2026-05-12 16:12:44.74+00:00")},
		{name: "named UTC", stored: validString("2026-08-19 18:57:50.92 +0000 UTC"), want: validString("2026-08-19 18:57:50.92+00:00")},
		{name: "crosses midnight", stored: validString("2026-08-24 22:30:00 -0400 -0400"), want: validString("2026-08-25 02:30:00+00:00")},
		{name: "positive half-hour offset", stored: validString("2026-08-24 01:00:00.5 +0530 +0530"), want: validString("2026-08-23 19:30:00.5+00:00")},
		{name: "already in the new format", stored: validString("2026-08-24 17:15:00+00:00"), want: validString("2026-08-24 17:15:00+00:00")},
		{name: "NULL", stored: sql.NullString{}, want: sql.NullString{}},
	}
	for i, tt := range tests {
		if _, err := sqlDB.Exec(
			`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload, published_at)
			 VALUES (?, 1, 'greenhouse', ?, 'Engineer', '{}', ?)`,
			i+1, fmt.Sprintf("job-%d", i+1), tt.stored,
		); err != nil {
			t.Fatalf("%s: insert posting: %v", tt.name, err)
		}
	}

	if err := goose.UpTo(sqlDB, ".", 12); err != nil {
		t.Fatalf("migrate to version 12: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 12 {
		t.Fatalf("DB version after UpTo(12) = %d, want 12 (migration 00012 not found?)", gotVersion)
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got sql.NullString
			if err := sqlDB.QueryRow(`SELECT CAST(published_at AS TEXT) FROM postings WHERE id = ?`, i+1).Scan(&got); err != nil {
				t.Fatalf("query posting: %v", err)
			}
			if got != tt.want {
				t.Errorf("published_at = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestApplicationStatusHistory_BackfillsExistingApplications verifies the
// 00014 migration's backfill (#162): every existing application gets an
// application_started row at its created_at, plus a row for its current
// status at its updated_at when that isn't application_started. What
// happened in between was never recorded and can't be recovered.
func TestApplicationStatusHistory_BackfillsExistingApplications(t *testing.T) {
	sqlDB := migrateTo(t, 13)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	applications := []struct {
		id        int
		status    string
		createdAt string
		updatedAt string
	}{
		{id: 1, status: "application_started", createdAt: "2026-09-01 10:00:00", updatedAt: "2026-09-02 11:00:00"},
		{id: 2, status: "posting_closed", createdAt: "2026-09-03 10:00:00", updatedAt: "2026-09-20 12:30:00"},
	}
	for _, a := range applications {
		if _, err := sqlDB.Exec(
			`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload)
			 VALUES (?, 1, 'ashby', ?, 'Engineer', '{}')`,
			a.id, fmt.Sprintf("job-%d", a.id),
		); err != nil {
			t.Fatalf("insert posting %d: %v", a.id, err)
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO applications (id, posting_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			a.id, a.id, a.status, a.createdAt, a.updatedAt,
		); err != nil {
			t.Fatalf("insert application %d: %v", a.id, err)
		}
	}

	if err := goose.UpTo(sqlDB, ".", 14); err != nil {
		t.Fatalf("migrate to version 14: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 14 {
		t.Fatalf("DB version after UpTo(14) = %d, want 14 (migration 00014 not found?)", gotVersion)
	}

	type historyRow struct {
		ApplicationID int
		Status        string
		ChangedAt     string
	}
	rows, err := sqlDB.Query(
		`SELECT application_id, status, CAST(changed_at AS TEXT) FROM application_status_history ORDER BY application_id, changed_at`,
	)
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []historyRow
	for rows.Next() {
		var r historyRow
		if err := rows.Scan(&r.ApplicationID, &r.Status, &r.ChangedAt); err != nil {
			t.Fatalf("scan history: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate history: %v", err)
	}

	want := []historyRow{
		{ApplicationID: 1, Status: "application_started", ChangedAt: "2026-09-01 10:00:00"},
		{ApplicationID: 2, Status: "application_started", ChangedAt: "2026-09-03 10:00:00"},
		{ApplicationID: 2, Status: "posting_closed", ChangedAt: "2026-09-20 12:30:00"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("application_status_history mismatch (-want +got):\n%s", diff)
	}
}

// TestApplicationStatusHistory_DownDropsTable verifies 00014's Down leaves
// the schema as 00013 had it: the history table is gone and the
// applications it recorded are untouched.
func TestApplicationStatusHistory_DownDropsTable(t *testing.T) {
	sqlDB := migrateTo(t, 14)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
	); err != nil {
		t.Fatalf("insert company: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload) VALUES (1, 1, 'ashby', 'job-1', 'Engineer', '{}')`,
	); err != nil {
		t.Fatalf("insert posting: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO applications (id, posting_id, status) VALUES (1, 1, 'application_started')`); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO application_status_history (application_id, status) VALUES (1, 'application_started')`); err != nil {
		t.Fatalf("insert history: %v", err)
	}

	if err := goose.DownTo(sqlDB, ".", 13); err != nil {
		t.Fatalf("migrate down to version 13: %v", err)
	}

	var tables int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'application_status_history'`,
	).Scan(&tables); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Errorf("application_status_history still exists after Down")
	}
	var applications int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM applications`).Scan(&applications); err != nil {
		t.Fatalf("count applications: %v", err)
	}
	if applications != 1 {
		t.Errorf("applications = %d after Down, want 1", applications)
	}
}

// TestCloseDeletedCompaniesPostings_ClosesOnlyDeletedCompaniesOpenPostings
// verifies the 00015 migration (#178): open postings of an already
// soft-deleted company are closed, each with a "closed" posting_history
// row snapshotting it as it was, the same as store.SoftDeleteCompany now
// does. Everything else is left alone.
func TestCloseDeletedCompaniesPostings_ClosesOnlyDeletedCompaniesOpenPostings(t *testing.T) {
	sqlDB := migrateTo(t, 14)

	if _, err := sqlDB.Exec(
		`INSERT INTO companies (id, name, source, source_ref, deleted_at) VALUES
		 (1, 'Deleted', 'ashby', 'deleted', '2026-09-29 10:00:00'),
		 (2, 'Active', 'ashby', 'active', NULL)`,
	); err != nil {
		t.Fatalf("insert companies: %v", err)
	}
	postings := []struct {
		id            int
		companyID     int
		listingStatus string
	}{
		{id: 1, companyID: 1, listingStatus: "open"},
		{id: 2, companyID: 1, listingStatus: "closed"},
		{id: 3, companyID: 2, listingStatus: "open"},
	}
	for _, p := range postings {
		if _, err := sqlDB.Exec(
			`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload, listing_status)
			 VALUES (?, ?, 'ashby', ?, 'Engineer', '{}', ?)`,
			p.id, p.companyID, fmt.Sprintf("job-%d", p.id), p.listingStatus,
		); err != nil {
			t.Fatalf("insert posting %d: %v", p.id, err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO applications (posting_id, status) VALUES (1, 'application_started')`); err != nil {
		t.Fatalf("insert application: %v", err)
	}

	if err := goose.UpTo(sqlDB, ".", 15); err != nil {
		t.Fatalf("migrate to version 15: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 15 {
		t.Fatalf("DB version after UpTo(15) = %d, want 15 (migration 00015 not found?)", gotVersion)
	}

	tests := []struct {
		name        string
		postingID   int
		wantStatus  string
		wantHistory int
	}{
		{name: "deleted company, open: closed", postingID: 1, wantStatus: "closed", wantHistory: 1},
		{name: "deleted company, already closed: untouched", postingID: 2, wantStatus: "closed", wantHistory: 0},
		{name: "active company: untouched", postingID: 3, wantStatus: "open", wantHistory: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var status string
			if err := sqlDB.QueryRow(`SELECT listing_status FROM postings WHERE id = ?`, tt.postingID).Scan(&status); err != nil {
				t.Fatalf("query posting: %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("listing_status = %q, want %q", status, tt.wantStatus)
			}
			var history int
			if err := sqlDB.QueryRow(
				`SELECT COUNT(*) FROM posting_history WHERE posting_id = ? AND change_type = 'closed'`, tt.postingID,
			).Scan(&history); err != nil {
				t.Fatalf("count history: %v", err)
			}
			if history != tt.wantHistory {
				t.Errorf("closed history rows = %d, want %d", history, tt.wantHistory)
			}
		})
	}

	var snapshot string
	if err := sqlDB.QueryRow(`SELECT snapshot FROM posting_history WHERE posting_id = 1`).Scan(&snapshot); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	for _, want := range []string{`"ListingStatus":"open"`, `"Title":"Engineer"`, `"SourceID":"job-1"`} {
		if !strings.Contains(snapshot, want) {
			t.Errorf("snapshot %s does not contain %s", snapshot, want)
		}
	}
	var appStatus string
	if err := sqlDB.QueryRow(`SELECT status FROM applications WHERE posting_id = 1`).Scan(&appStatus); err != nil {
		t.Fatalf("query application: %v", err)
	}
	if appStatus != "application_started" {
		t.Errorf("application status = %q, want it left at application_started", appStatus)
	}
}

func validString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// TestApplicationStatusChangedBy_ExistingRowsUnknown verifies the 00018
// migration (#174): history rows written before it can't say whether the
// user or sync made the change, so they're marked unknown. Sync only
// undoes a posting_closed it's known to have made, so an unknown row is
// never restored.
func TestApplicationStatusChangedBy_ExistingRowsUnknown(t *testing.T) {
	sqlDB := migrateTo(t, 17)

	for _, stmt := range []string{
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload) VALUES (1, 1, 'ashby', 'job-1', 'Engineer', '{}')`,
		`INSERT INTO applications (id, posting_id, status) VALUES (1, 1, 'posting_closed')`,
		`INSERT INTO application_status_history (application_id, status) VALUES (1, 'application_started'), (1, 'posting_closed')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := goose.UpTo(sqlDB, ".", 18); err != nil {
		t.Fatalf("migrate to version 18: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 18 {
		t.Fatalf("DB version after UpTo(18) = %d, want 18 (migration 00018 not found?)", gotVersion)
	}

	rows, err := sqlDB.Query(`SELECT changed_by FROM application_status_history ORDER BY id`)
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var changedBy string
		if err := rows.Scan(&changedBy); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, changedBy)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if diff := cmp.Diff([]string{"unknown", "unknown"}, got); diff != "" {
		t.Errorf("changed_by mismatch (-want +got):\n%s", diff)
	}
}

func TestApplicationsAutoincrement_KeepsRowsAndContinuesFromMaxID(t *testing.T) {
	sqlDB := migrateTo(t, 18)

	for _, stmt := range []string{
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload) VALUES
			(1, 1, 'ashby', 'job-1', 'Engineer', '{}'),
			(2, 1, 'ashby', 'job-2', 'Staff Engineer', '{}'),
			(3, 1, 'ashby', 'job-3', 'Manager', '{}')`,
		`INSERT INTO applications (id, posting_id, status, notes, created_at, updated_at) VALUES
			(1, 1, 'application_started', 'first', '2026-09-01 10:00:00', '2026-09-02 11:00:00'),
			(5, 2, 'interviewing', '', '2026-09-03 12:00:00', '2026-09-04 13:00:00')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	type row struct {
		ID, PostingID                   int64
		Status, Notes, Created, Updated string
	}
	read := func() []row {
		t.Helper()
		rows, err := sqlDB.Query(`SELECT id, posting_id, status, notes, created_at, updated_at FROM applications ORDER BY id`)
		if err != nil {
			t.Fatalf("query applications: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var got []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.ID, &r.PostingID, &r.Status, &r.Notes, &r.Created, &r.Updated); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got = append(got, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return got
	}
	before := read()

	if err := goose.UpTo(sqlDB, ".", 19); err != nil {
		t.Fatalf("migrate to version 19: %v", err)
	}
	if gotVersion, err := goose.GetDBVersion(sqlDB); err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	} else if gotVersion != 19 {
		t.Fatalf("DB version after UpTo(19) = %d, want 19 (migration 00019 not found?)", gotVersion)
	}

	if diff := cmp.Diff(before, read()); diff != "" {
		t.Errorf("applications changed by migration (-before +after):\n%s", diff)
	}

	if _, err := sqlDB.Exec(`DELETE FROM applications WHERE id = 5`); err != nil {
		t.Fatalf("delete max application: %v", err)
	}
	var nextID int64
	if err := sqlDB.QueryRow(`INSERT INTO applications (posting_id, status) VALUES (3, 'application_started') RETURNING id`).Scan(&nextID); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if nextID != 6 {
		t.Errorf("next application id = %d, want 6 (deleted max id 5 must not be reused)", nextID)
	}
}

func TestApplicationsAutoincrement_DownKeepsRows(t *testing.T) {
	sqlDB := migrateTo(t, 19)

	for _, stmt := range []string{
		`INSERT INTO companies (id, name, source, source_ref) VALUES (1, 'Acme', 'ashby', 'acme')`,
		`INSERT INTO postings (id, company_id, source, source_id, title, raw_payload) VALUES (1, 1, 'ashby', 'job-1', 'Engineer', '{}')`,
		`INSERT INTO applications (id, posting_id, status, notes) VALUES (7, 1, 'interviewing', 'kept')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := goose.DownTo(sqlDB, ".", 18); err != nil {
		t.Fatalf("migrate down to version 18: %v", err)
	}

	var id, postingID int64
	var status, notes string
	if err := sqlDB.QueryRow(`SELECT id, posting_id, status, notes FROM applications`).Scan(&id, &postingID, &status, &notes); err != nil {
		t.Fatalf("read application: %v", err)
	}
	got := fmt.Sprintf("%d %d %s %s", id, postingID, status, notes)
	if want := "7 1 interviewing kept"; got != want {
		t.Errorf("application after down = %q, want %q", got, want)
	}
}
