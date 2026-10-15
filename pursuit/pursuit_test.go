package pursuit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/pursuit"
	"github.com/dklassen/swamp/store"
)

// testStore keeps the *sql.DB beside the store, for tests that write past
// the areas.
type testStore struct {
	*store.Store
	sqlDB *sql.DB
}

func newTestStore(t *testing.T) testStore {
	t.Helper()
	sqlDB, err := store.Open(t.TempDir()+"/test.db", store.DefaultConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return testStore{Store: store.New(sqlDB), sqlDB: sqlDB}
}

func newApplication(t *testing.T, s testStore) application.Application {
	t.Helper()
	ctx := context.Background()
	c, err := s.CreateCompany(ctx, "Acme", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	p, err := s.UpsertPosting(ctx, store.CreatePostingParams{
		CompanyID: c.ID, Source: "ashby", SourceID: "job-1",
		IngestedFields: store.IngestedFields{Title: "Software Engineer", RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	app, err := createApplication(ctx, s, p.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	return app
}

func TestList_NoReviews_LatestReviewsEmpty(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	newApplication(t, s)

	got, err := pursuit.List(context.Background(), s.DB())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].LatestReviews) != 0 {
		t.Fatalf("LatestReviews = %+v, want empty (no reviews recorded)", got[0].LatestReviews)
	}
}

func TestList_IncludesLatestDocumentReviews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	app := newApplication(t, s)
	if _, err := createReview(ctx, s, app.ID, documents.CoverLetter, "draft one", docrecord.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview (first): %v", err)
	}
	second, err := createReview(ctx, s, app.ID, documents.CoverLetter, "draft two", docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview (second): %v", err)
	}

	got, err := pursuit.List(ctx, s.DB())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	review, ok := got[0].LatestReviews[documents.CoverLetter]
	if !ok {
		t.Fatalf("LatestReviews[%q] missing, want the most recent review present", documents.CoverLetter)
	}
	if review.ID != second.ID {
		t.Fatalf("LatestReviews[%q].ID = %d, want %d (most recent cycle, not the first)", documents.CoverLetter, review.ID, second.ID)
	}
	if _, ok := got[0].LatestReviews[documents.Resume]; ok {
		t.Fatalf("LatestReviews[%q] present, want absent (no resume review recorded)", documents.Resume)
	}
}

func createReview(ctx context.Context, s testStore, applicationID int64, documentType documents.Type, content string, outcome docrecord.Outcome, notes string) (docrecord.Review, error) {
	var review docrecord.Review
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		review, err = docrecord.CreateReview(ctx, tx, applicationID, documentType, content, outcome, notes)
		return err
	})
	return review, err
}

func createApplication(ctx context.Context, s testStore, postingID int64) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.Create(ctx, tx, postingID)
		return err
	})
	return app, err
}

func mustCreateCompany(t *testing.T, s testStore, name, source, sourceRef string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s testStore, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID, Source: "ashby", SourceID: sourceID,
		IngestedFields: store.IngestedFields{Title: title, RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func mustCreateApplication(t *testing.T, s testStore, postingID int64) application.Application {
	t.Helper()
	a, err := createApplication(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return a
}

func updateApplicationStatus(ctx context.Context, s testStore, postingID int64, status applicationstatus.Status) (application.Application, error) {
	var app application.Application
	err := store.InTx(ctx, s.DB(), func(tx store.Tx) error {
		var err error
		app, err = application.UpdateStatus(ctx, tx, postingID, status)
		return err
	})
	return app, err
}

func TestList_ExcludesRejectedAndOfferDeclined(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	started := mustUpsertPosting(t, s, acme.ID, "job-started", "Started Role")
	mustCreateApplication(t, s, started.ID)

	interviewing := mustUpsertPosting(t, s, acme.ID, "job-interviewing", "Interviewing Role")
	mustCreateApplication(t, s, interviewing.ID)
	if _, err := updateApplicationStatus(ctx, s, interviewing.ID, applicationstatus.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	rejected := mustUpsertPosting(t, s, acme.ID, "job-rejected", "Rejected Role")
	mustCreateApplication(t, s, rejected.ID)
	if _, err := updateApplicationStatus(ctx, s, rejected.ID, applicationstatus.Rejected); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	declined := mustUpsertPosting(t, s, acme.ID, "job-declined", "Declined Role")
	mustCreateApplication(t, s, declined.ID)
	if _, err := updateApplicationStatus(ctx, s, declined.ID, applicationstatus.OfferDeclined); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	noApplication := mustUpsertPosting(t, s, acme.ID, "job-no-app", "No Application Role")
	_ = noApplication

	got, err := pursuit.List(ctx, s.DB())
	if err != nil {
		t.Fatalf("pursuit.List: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (started + interviewing only): %+v", len(got), got)
	}
	gotTitles := map[string]bool{}
	for _, v := range got {
		gotTitles[v.Posting.Title] = true
	}
	if !gotTitles["Started Role"] || !gotTitles["Interviewing Role"] {
		t.Fatalf("got titles = %v, want Started Role and Interviewing Role present", gotTitles)
	}
	if gotTitles["Rejected Role"] || gotTitles["Declined Role"] || gotTitles["No Application Role"] {
		t.Fatalf("got titles = %v, want rejected/declined/no-application excluded", gotTitles)
	}
}

// TestList_MatchesTerminalStatuses is a
// drift-detection test, not just a regression test: it derives "which
// statuses should be excluded" from applicationstatus.Terminal() itself
// rather than hardcoding rejected/offer_declined a second time, so it
// stays correct (and would catch a real mismatch) even if the set of
// terminal statuses changes later.
func TestList_MatchesTerminalStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	terminal := map[applicationstatus.Status]bool{}
	for _, status := range applicationstatus.Terminal() {
		terminal[status] = true
	}

	wantIncluded := map[string]bool{}
	for _, status := range applicationstatus.All() {
		posting := mustUpsertPosting(t, s, acme.ID, "job-"+status.String(), status.String())
		mustCreateApplication(t, s, posting.ID)
		if _, err := updateApplicationStatus(ctx, s, posting.ID, status); err != nil {
			t.Fatalf("application.UpdateStatus(%s): %v", status, err)
		}
		if !terminal[status] {
			wantIncluded[posting.Title] = true
		}
	}

	got, err := pursuit.List(ctx, s.DB())
	if err != nil {
		t.Fatalf("pursuit.List: %v", err)
	}

	gotTitles := map[string]bool{}
	for _, v := range got {
		gotTitles[v.Posting.Title] = true
	}
	if len(gotTitles) != len(wantIncluded) {
		t.Fatalf("pursuit.List returned %v, want exactly %v", gotTitles, wantIncluded)
	}
	for title := range wantIncluded {
		if !gotTitles[title] {
			t.Fatalf("pursuit.List missing %q (non-terminal status), got %v", title, gotTitles)
		}
	}
}

func TestList_IncludesCompanyNameAndApplicationFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	got, err := pursuit.List(ctx, s.DB())
	if err != nil {
		t.Fatalf("pursuit.List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	v := got[0]
	if v.CompanyName != "Acme" {
		t.Fatalf("CompanyName = %q, want %q", v.CompanyName, "Acme")
	}
	if v.Posting.Title != "Software Engineer" {
		t.Fatalf("Posting.Title = %q, want %q", v.Posting.Title, "Software Engineer")
	}
	if v.ID != app.ID {
		t.Fatalf("ID = %d, want %d", v.ID, app.ID)
	}
	if v.Status != applicationstatus.Started {
		t.Fatalf("Status = %s, want %s", v.Status, applicationstatus.Started)
	}
	if v.Notes != app.Notes {
		t.Fatalf("Notes = %q, want %q", v.Notes, app.Notes)
	}
	if v.PostingID != posting.ID {
		t.Fatalf("PostingID = %d, want %d", v.PostingID, posting.ID)
	}
}

// StatusSince is when each application entered its current status,
// from status history, so the home screen can show how long it's
// been stuck there. An application with no history falls back to
// CreatedAt.
func TestList_StatusSince(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	withHistory := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	app := mustCreateApplication(t, s, withHistory.ID)
	if _, err := updateApplicationStatus(ctx, s, withHistory.ID, applicationstatus.Submitted); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	startedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	submittedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if _, err := s.sqlDB.ExecContext(ctx,
		`UPDATE application_status_history SET changed_at = ? WHERE application_id = ? AND status = 'application_submitted'`,
		submittedAt, app.ID); err != nil {
		t.Fatalf("set changed_at: %v", err)
	}
	if _, err := s.sqlDB.ExecContext(ctx,
		`UPDATE application_status_history SET changed_at = ? WHERE application_id = ? AND status = 'application_started'`,
		startedAt, app.ID); err != nil {
		t.Fatalf("set changed_at: %v", err)
	}

	noHistory := mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	bare := mustCreateApplication(t, s, noHistory.ID)
	if _, err := s.sqlDB.ExecContext(ctx, `DELETE FROM application_status_history WHERE application_id = ?`, bare.ID); err != nil {
		t.Fatalf("delete history: %v", err)
	}

	views, err := pursuit.List(ctx, s.DB())
	if err != nil {
		t.Fatalf("pursuit.List: %v", err)
	}
	got := map[int64]time.Time{}
	for _, v := range views {
		got[v.ID] = v.StatusSince
	}
	if !got[app.ID].Equal(submittedAt) {
		t.Errorf("StatusSince with history = %v, want %v (when it entered application_submitted)", got[app.ID], submittedAt)
	}
	if !got[bare.ID].Equal(bare.CreatedAt) {
		t.Errorf("StatusSince without history = %v, want CreatedAt %v", got[bare.ID], bare.CreatedAt)
	}
}
