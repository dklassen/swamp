package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func TestApplicationService_SetStatus_UpdatesStatus(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	applications := s.Applications
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	updated, err := applications.SetStatus(ctx, app.ID, application.Interviewing)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if updated.Status != application.Interviewing {
		t.Errorf("Status = %s, want %s", updated.Status, application.Interviewing)
	}
	got, err := applications.Get(ctx, app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Errorf("stored application (-returned +stored):\n%s", diff)
	}
}

func TestApplicationService_SetStatus_NonexistentApplication_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)

	if _, err := s.Applications.SetStatus(context.Background(), 999, application.Interviewing); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetStatus(999): err = %v, want store.ErrNotFound", err)
	}
}

// Setting the status an application already has isn't a change; recording
// it would move when the application entered that status.
func TestApplicationService_SetStatus_SameStatus_RecordsNothing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	applications := s.Applications
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	if _, err := applications.SetStatus(ctx, app.ID, app.Status); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	history, err := applications.History(ctx, app.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history = %+v, want only ApplyTo's row", history)
	}
}

// ApplyTo and SetStatus record the user's changes and Close records
// sync's, which is what lets Reopen undo only what Close did.
func TestApplicationService_History_RecordsWhoChangedIt(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	applications := s.Applications
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	app := testutil.MustApplyTo(t, s, p.ID)
	if _, err := applications.SetStatus(ctx, app.ID, application.Submitted); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if _, err := applications.SetStatus(ctx, app.ID, application.Started); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if _, _, err := s.Postings.Close(ctx, p.ID, []application.Status{application.Started}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	history, err := applications.History(ctx, app.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	type change struct {
		Status    application.Status
		ChangedBy application.ChangedBy
	}
	got := make([]change, len(history))
	for i, h := range history {
		got[i] = change{h.Status, h.ChangedBy}
	}
	want := []change{
		{application.Started, application.ChangedByUser},
		{application.Submitted, application.ChangedByUser},
		{application.Started, application.ChangedByUser},
		{application.PostingClosed, application.ChangedBySync},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("history (-want +got):\n%s", diff)
	}
}

func TestApplicationService_Active_IncludesThePostingAndCompany(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	app := testutil.MustApplyTo(t, s, p.ID)

	got, err := s.Applications.Active(context.Background())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	want := []posting.ActiveApplication{{Application: app, Posting: p, CompanyName: "Acme"}}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(posting.ActiveApplication{}, "StatusSince")); diff != "" {
		t.Errorf("Active (-want +got):\n%s", diff)
	}
}

func TestApplicationService_Active_IncludesTheLatestReviewPerDocument(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	if _, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft one", document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review (first): %v", err)
	}
	second, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft two", document.Passed, "")
	if err != nil {
		t.Fatalf("Review (second): %v", err)
	}

	got, err := s.Applications.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Active = %d applications, want 1", len(got))
	}
	want := map[document.Type]document.Review{document.CoverLetter: second}
	if diff := cmp.Diff(want, got[0].LatestReviews); diff != "" {
		t.Errorf("LatestReviews (-want +got):\n%s", diff)
	}
}

// The excluded statuses come from application.Terminal, so the test stays
// right if the terminal set changes.
func TestApplicationService_Active_LeavesOutTerminalStatusesAndPostingsWithoutAnApplication(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	applications := s.Applications
	acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	ingest := func(title string) posting.Posting {
		t.Helper()
		ingested, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-"+title, posting.IngestedFields{Title: title, RawPayload: "{}"})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		return ingested.Posting
	}

	var want []string
	for _, status := range application.All() {
		p := ingest(status.String())
		app := testutil.MustApplyTo(t, s, p.ID)
		if _, err := applications.SetStatus(ctx, app.ID, status); err != nil {
			t.Fatalf("SetStatus(%s): %v", status, err)
		}
		if !slices.Contains(application.Terminal(), status) {
			want = append(want, p.Title)
		}
	}
	ingest("no application")

	got, err := applications.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	var titles []string
	for _, a := range got {
		titles = append(titles, a.Posting.Title)
	}
	if diff := cmp.Diff(want, titles, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("active application titles (-want +got):\n%s", diff)
	}
}
