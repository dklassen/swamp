package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
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
	if updated.After.Status != application.Interviewing {
		t.Errorf("Status = %s, want %s", updated.After.Status, application.Interviewing)
	}
	got, err := applications.Get(ctx, app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(updated.After, *got); diff != "" {
		t.Errorf("stored application (-returned +stored):\n%s", diff)
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
	if err := s.Postings.Close(ctx, p.ID, []application.Status{application.Started}); err != nil {
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
	want := map[document.Type]document.Review{document.CoverLetter: *second}
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

// Before is read in the write's transaction, so it's what the last writer
// left, not what the caller last saw.
func TestApplicationService_Updates_ReturnTheApplicationBeforeAndAfter(t *testing.T) {
	t.Parallel()
	writes := []struct {
		name  string
		write func(*service.ApplicationService, context.Context, int64) (*change.Of[application.Application], error)
	}{
		{"SetStatus", func(s *service.ApplicationService, ctx context.Context, id int64) (*change.Of[application.Application], error) {
			return s.SetStatus(ctx, id, application.Interviewing)
		}},
		{"SetNotes", func(s *service.ApplicationService, ctx context.Context, id int64) (*change.Of[application.Application], error) {
			return s.SetNotes(ctx, id, "Recruiter called back")
		}},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			if _, err := s.Applications.SetNotes(ctx, app.ID, "Referred by a friend"); err != nil {
				t.Fatalf("SetNotes: %v", err)
			}
			before, err := s.Applications.Get(ctx, app.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}

			got, err := w.write(s.Applications, ctx, app.ID)
			if err != nil {
				t.Fatalf("%s: %v", w.name, err)
			}

			if diff := cmp.Diff(*before, got.Before); diff != "" {
				t.Errorf("Before (-want +got):\n%s", diff)
			}
			after, err := s.Applications.Get(ctx, app.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if diff := cmp.Diff(*after, got.After); diff != "" {
				t.Errorf("After (-want +got):\n%s", diff)
			}
			if !got.At.Equal(got.After.UpdatedAt) || got.At.Location() != time.UTC {
				t.Errorf("At = %v, want After.UpdatedAt %v, in UTC", got.At, got.After.UpdatedAt)
			}
		})
	}
}

// After is the soft-deleted row, and At when it was deleted.
func TestApplicationService_Delete_ReturnsTheApplicationBeforeAndAfter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	if _, err := s.Applications.SetNotes(ctx, app.ID, "Referred by a friend"); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	before, err := s.Applications.Get(ctx, app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	got, err := s.Applications.Delete(ctx, app.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if diff := cmp.Diff(*before, got.Before); diff != "" {
		t.Errorf("Before (-want +got):\n%s", diff)
	}
	after, err := s.Applications.GetIncludingDeleted(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetIncludingDeleted: %v", err)
	}
	if diff := cmp.Diff(*after, got.After); diff != "" {
		t.Errorf("After (-want +got):\n%s", diff)
	}
	if got.After.DeletedAt.IsZero() {
		t.Fatal("After.DeletedAt is zero, want when it was deleted")
	}
	if !got.At.Equal(got.After.DeletedAt) || got.At.Location() != time.UTC {
		t.Errorf("At = %v, want After.DeletedAt %v, in UTC", got.At, got.After.DeletedAt)
	}
}

func TestApplicationService_Writes_ReturnNotFoundForAnApplicationNotThere(t *testing.T) {
	t.Parallel()
	writes := []struct {
		name  string
		write func(*service.ApplicationService, context.Context, int64) (*change.Of[application.Application], error)
	}{
		{"SetStatus", func(s *service.ApplicationService, ctx context.Context, id int64) (*change.Of[application.Application], error) {
			return s.SetStatus(ctx, id, application.Interviewing)
		}},
		{"SetNotes", func(s *service.ApplicationService, ctx context.Context, id int64) (*change.Of[application.Application], error) {
			return s.SetNotes(ctx, id, "Recruiter called back")
		}},
		{"Delete", (*service.ApplicationService).Delete},
	}
	applications := []struct {
		name    string
		arrange func(*testing.T, testutil.TestApp) int64
	}{
		{"missing", func(*testing.T, testutil.TestApp) int64 { return 999 }},
		{"already deleted", func(t *testing.T, s testutil.TestApp) int64 {
			t.Helper()
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			if _, err := s.Applications.Delete(context.Background(), app.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			return app.ID
		}},
	}
	for _, w := range writes {
		for _, a := range applications {
			t.Run(w.name+", "+a.name, func(t *testing.T) {
				t.Parallel()
				s := testutil.NewTestApp(t)
				id := a.arrange(t, s)

				got, err := w.write(s.Applications, context.Background(), id)

				if !errors.Is(err, store.ErrNotFound) {
					t.Errorf("err = %v, want store.ErrNotFound", err)
				}
				if got != nil {
					t.Errorf("change = %+v, want nil", got)
				}
			})
		}
	}
}

func TestApplicationService_GetManyIncludingDeleted_FindsDeletedLeavesOutMissing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	live, err := s.Postings.ApplyTo(ctx, testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Software Engineer").ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	gone, err := s.Postings.ApplyTo(ctx, testutil.MustIngestPostingFor(t, s, acme.ID, "job-2", "Designer").ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	deleted, err := s.Applications.Delete(ctx, gone.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Applications.GetManyIncludingDeleted(ctx, []int64{gone.ID, 9999, live.ID})
	if err != nil {
		t.Fatalf("GetManyIncludingDeleted: %v", err)
	}
	want := []application.Application{*live, deleted.After}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b application.Application) bool { return a.ID < b.ID })); diff != "" {
		t.Errorf("GetManyIncludingDeleted (-want +got):\n%s", diff)
	}
}
