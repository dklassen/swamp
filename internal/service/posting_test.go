package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// Close only ends the application of a posting it closes, so one started
// on an already-closed posting would sit at application_started for good.
func TestPostingService_ApplyTo_ClosedPosting_Refused(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	if err := postings.Close(ctx, p.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := postings.ApplyTo(ctx, p.ID); !errors.Is(err, service.ErrPostingClosed) {
		t.Fatalf("ApplyTo on a closed posting: err = %v, want service.ErrPostingClosed", err)
	}
	if _, err := postings.Application(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Application after the refusal: err = %v, want store.ErrNotFound", err)
	}
}

// Foreign keys aren't enforced, so without the posting check ApplyTo
// would start an application for a posting that doesn't exist.
func TestPostingService_ApplyTo_NonexistentPosting_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)

	if _, err := s.Postings.ApplyTo(context.Background(), 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ApplyTo(9999): err = %v, want store.ErrNotFound", err)
	}
}

func TestPostingService_ApplyTo_RecordsStartedInHistoryAsTheUsersChange(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	app, err := s.Postings.ApplyTo(ctx, p.ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}

	history, err := s.Applications.History(ctx, app.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	want := []application.StatusChange{{ApplicationID: app.ID, Status: application.Started, ChangedBy: application.ChangedByUser}}
	if diff := cmp.Diff(want, history, cmpopts.IgnoreFields(application.StatusChange{}, "ID", "ChangedAt")); diff != "" {
		t.Errorf("history (-want +got):\n%s", diff)
	}
	for i, change := range history {
		if change.ChangedAt.IsZero() {
			t.Errorf("history row %d has a zero ChangedAt", i)
		}
	}
}

// An application submitted before its posting closed keeps its
// submission in history.
func TestPostingService_Close_KeepsSubmittedThenClosedInHistory(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	app := testutil.MustApplyTo(t, s, p.ID)
	applications := s.Applications
	if _, err := applications.SetStatus(ctx, app.ID, application.Submitted); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	if err := s.Postings.Close(ctx, p.ID, []application.Status{application.Started, application.Submitted}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	history, err := applications.History(ctx, app.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []application.Status
	for _, change := range history {
		got = append(got, change.Status)
	}
	want := []application.Status{application.Started, application.Submitted, application.PostingClosed}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("history (-want +got):\n%s", diff)
	}
}

func TestPostingService_Ingest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stored      string // title already stored; "" means not stored yet
		ingest      string
		wantCreated bool
		wantUpdated bool
		wantHistory []string
	}{
		{name: "not stored: created", ingest: "Engineer", wantCreated: true},
		{name: "stored, same content: nothing changes", stored: "Engineer", ingest: "Engineer"},
		{
			name: "stored, new content: updated with history", stored: "Engineer", ingest: "Senior Engineer",
			wantUpdated: true, wantHistory: []string{posting.ChangeContentUpdated},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			postings := s.Postings
			acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			var before posting.Posting
			if tt.stored != "" {
				stored, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{Title: tt.stored, RawPayload: "{}"})
				if err != nil {
					t.Fatalf("Ingest the stored version: %v", err)
				}
				before = stored.Posting
			}

			got, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{Title: tt.ingest, RawPayload: "{}"})
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if got.Created != tt.wantCreated || got.Updated != tt.wantUpdated {
				t.Errorf("Created, Updated = %v, %v, want %v, %v", got.Created, got.Updated, tt.wantCreated, tt.wantUpdated)
			}
			if got.Posting.Title != tt.ingest {
				t.Errorf("Posting.Title = %q, want %q", got.Posting.Title, tt.ingest)
			}
			if tt.stored == tt.ingest && !got.Posting.UpdatedAt.Equal(before.UpdatedAt) {
				t.Errorf("UpdatedAt changed from %v to %v, want the row untouched", before.UpdatedAt, got.Posting.UpdatedAt)
			}

			history, err := postings.History(ctx, got.Posting.ID)
			if err != nil {
				t.Fatalf("History: %v", err)
			}
			var changeTypes []string
			for _, h := range history {
				changeTypes = append(changeTypes, h.ChangeType)
				var snapshot posting.Posting
				if err := json.Unmarshal([]byte(h.Snapshot), &snapshot); err != nil {
					t.Fatalf("decode snapshot %s: %v", h.Snapshot, err)
				}
				if snapshot.Title != tt.stored {
					t.Errorf("history snapshot has Title %q, want the stored version's %q", snapshot.Title, tt.stored)
				}
			}
			if diff := cmp.Diff(tt.wantHistory, changeTypes); diff != "" {
				t.Errorf("history (-want +got):\n%s", diff)
			}
		})
	}
}

// Every posting has exactly one markup row.
func TestPostingService_Ingest_NewPosting_CreatesAnUntriagedMarkupRow(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	markup, err := s.Postings.Markup(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	want := &posting.Markup{PostingID: p.ID}
	if diff := cmp.Diff(want, markup, cmpopts.IgnoreFields(posting.Markup{}, "CreatedAt", "UpdatedAt")); diff != "" {
		t.Errorf("markup (-want +got):\n%s", diff)
	}
}

// Opening and closing a posting are explicit decisions, made by Close and
// Reopen, never a side effect of re-ingesting its content.
func TestPostingService_Ingest_NewContentOnAClosedPosting_LeavesItClosed(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	if err := postings.Close(ctx, p.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	closed, err := postings.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	updated, err := postings.Ingest(ctx, p.CompanyID, p.Source, p.SourceID, posting.IngestedFields{Title: "Software Engineer II", RawPayload: "{}"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !updated.Updated {
		t.Fatal("Ingest didn't store the new content")
	}
	if updated.Posting.ListingStatus != closed.ListingStatus {
		t.Errorf("ListingStatus after Ingest = %q, want %q: only Reopen opens a posting", updated.Posting.ListingStatus, closed.ListingStatus)
	}
}

func TestPostingService_Ingest_SameSourceID_UpdatesInPlace(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	first := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	second, err := postings.Ingest(ctx, first.CompanyID, first.Source, first.SourceID, posting.IngestedFields{Title: "Senior Software Engineer", RawPayload: "{}"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if second.Posting.ID != first.ID {
		t.Errorf("Ingest's posting ID = %d, want %d (the same posting)", second.Posting.ID, first.ID)
	}
	all, err := postings.ListByCompany(ctx, first.CompanyID)
	if err != nil {
		t.Fatalf("ListByCompany: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ListByCompany = %d postings, want 1: a re-fetch must not duplicate the posting", len(all))
	}
}

// A score is tied to the content hash, so the hash changes with the
// posting's described role and not with its raw payload or URLs.
func TestPostingService_Ingest_ContentHashFollowsTheDescribedRole(t *testing.T) {
	t.Parallel()

	fields := posting.IngestedFields{
		Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid",
		DescriptionText: "Build things.", JobURL: "https://jobs.example.com/1", RawPayload: `{"v":1}`,
	}
	tests := []struct {
		name        string
		change      func(f *posting.IngestedFields)
		wantChanged bool
	}{
		{"title", func(f *posting.IngestedFields) { f.Title = "Senior Engineer" }, true},
		{"description", func(f *posting.IngestedFields) { f.DescriptionText = "Build other things." }, true},
		{"raw payload only", func(f *posting.IngestedFields) { f.RawPayload = `{"v":2}` }, false},
		{"job URL only", func(f *posting.IngestedFields) { f.JobURL = "https://jobs.example.com/one" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			postings := s.Postings
			acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			created, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, "1", fields)
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if created.Posting.ContentSHA256 == "" {
				t.Fatal("new posting has no content hash")
			}
			changed := fields
			tt.change(&changed)
			updated, err := postings.Ingest(ctx, acme.ID, jobboard.Ashby, "1", changed)
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if !updated.Updated {
				t.Fatal("the change wasn't stored")
			}
			if got := updated.Posting.ContentSHA256 != created.Posting.ContentSHA256; got != tt.wantChanged {
				t.Errorf("changing the %s changed the hash: %v, want %v", tt.name, got, tt.wantChanged)
			}
			stored, err := postings.Get(ctx, created.Posting.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if stored.ContentSHA256 != updated.Posting.ContentSHA256 {
				t.Errorf("stored hash %q, Ingest returned %q", stored.ContentSHA256, updated.Posting.ContentSHA256)
			}
		})
	}
}

func TestPostingService_Reopen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		closed      bool
		wantHistory []string
	}{
		{
			name: "closed: reopened with history", closed: true,
			wantHistory: []string{posting.ChangeClosed, posting.ChangeReopened},
		},
		{name: "already open: changes nothing", closed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			postings := s.Postings
			open := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
			before := open
			if tt.closed {
				if err := postings.Close(ctx, open.ID, nil); err != nil {
					t.Fatalf("Close: %v", err)
				}
				closed, err := postings.Get(ctx, open.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				before = *closed
			}

			if err := postings.Reopen(ctx, open.ID); err != nil {
				t.Fatalf("Reopen: %v", err)
			}
			after, err := postings.Get(ctx, open.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if after.ListingStatus != open.ListingStatus {
				t.Errorf("ListingStatus = %q, want %q", after.ListingStatus, open.ListingStatus)
			}
			history, err := postings.History(ctx, open.ID)
			if err != nil {
				t.Fatalf("History: %v", err)
			}
			var changeTypes []string
			for _, h := range history {
				changeTypes = append(changeTypes, h.ChangeType)
			}
			if diff := cmp.Diff(tt.wantHistory, changeTypes); diff != "" {
				t.Fatalf("history (-want +got):\n%s", diff)
			}
			if tt.closed {
				var snapshot posting.Posting
				if err := json.Unmarshal([]byte(history[len(history)-1].Snapshot), &snapshot); err != nil {
					t.Fatalf("decode snapshot: %v", err)
				}
				if snapshot.ListingStatus != before.ListingStatus {
					t.Errorf("reopened snapshot has ListingStatus %q, want the posting as it was while closed (%q)", snapshot.ListingStatus, before.ListingStatus)
				}
			}
		})
	}
}

func TestPostingService_Interested_WithNoApplication_LeavesApplicationFieldsNil(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	if _, err := postings.SetInterested(ctx, p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}

	got, err := postings.Interested(ctx)
	if err != nil {
		t.Fatalf("Interested: %v", err)
	}
	want := []posting.Interested{{Posting: p, CompanyName: "Acme"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Interested (-want +got):\n%s", diff)
	}
}

func TestPostingService_Interested_WithApplication_IncludesApplicationFields(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	if _, err := postings.SetInterested(ctx, p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	app := testutil.MustApplyTo(t, s, p.ID)

	got, err := postings.Interested(ctx)
	if err != nil {
		t.Fatalf("Interested: %v", err)
	}
	want := []posting.Interested{{Posting: p, CompanyName: "Acme", ApplicationID: &app.ID, ApplicationStatus: &app.Status}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Interested (-want +got):\n%s", diff)
	}
}

// Sets archived_at directly, since SetArchived also clears interested_at:
// the list must leave out an archived posting whatever its interested_at.
func TestPostingService_Interested_ExcludesArchived(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	if _, err := postings.SetInterested(ctx, p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	if _, err := postings.SetArchived(ctx, p.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	got, err := postings.Interested(ctx)
	if err != nil {
		t.Fatalf("Interested: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Interested = %+v, want none: the posting is archived", got)
	}
}

// Interested leaves out a closed posting, a deleted company's posting and
// a posting its company's filters exclude.
func TestPostingService_Interested_ExcludesClosedDeletedAndFilteredOut(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		hide func(t *testing.T, s testutil.TestApp, companyID, postingID int64)
	}{
		{"closed posting", func(t *testing.T, s testutil.TestApp, _, postingID int64) {
			if err := s.Postings.Close(context.Background(), postingID, nil); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}},
		{"deleted company", func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			if _, err := s.Companies.Delete(context.Background(), companyID); err != nil {
				t.Fatalf("delete company: %v", err)
			}
		}},
		{"filtered out", func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			if err := s.Companies.SetFilters(context.Background(), companyID, []filter.Filter{{Field: filter.FieldDepartment, Value: "Sales"}}); err != nil {
				t.Fatalf("SetFilters: %v", err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			companies := s.Companies
			postings := s.Postings
			ingestInterested := func(name, sourceID string) (companyID, postingID int64) {
				c, err := companies.Add(ctx, name, jobboard.Ashby, name)
				if err != nil {
					t.Fatalf("Add: %v", err)
				}
				ingested, err := postings.Ingest(ctx, c.ID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: "Engineer", Department: "Engineering", RawPayload: "{}"})
				if err != nil {
					t.Fatalf("Ingest: %v", err)
				}
				if _, err := postings.SetInterested(ctx, ingested.Posting.ID); err != nil {
					t.Fatalf("SetInterested: %v", err)
				}
				return c.ID, ingested.Posting.ID
			}
			hiddenCompany, hidden := ingestInterested("Acme", "job-1")
			_, shown := ingestInterested("Globex", "job-2")
			tt.hide(t, s, hiddenCompany, hidden)

			got, err := postings.Interested(ctx)
			if err != nil {
				t.Fatalf("Interested: %v", err)
			}
			var ids []int64
			for _, p := range got {
				ids = append(ids, p.Posting.ID)
			}
			if diff := cmp.Diff([]int64{shown}, ids); diff != "" {
				t.Errorf("listed postings (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPostingService_ApplyTo_AfterTheApplicationIsDeleted_StartsAFreshOne(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	applications := s.Applications
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	interested, err := postings.SetInterested(ctx, p.ID)
	if err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	app := testutil.MustApplyTo(t, s, p.ID)
	postingBefore, err := postings.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if _, err := applications.Delete(ctx, app.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	postingAfter, err := postings.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if diff := cmp.Diff(postingBefore, postingAfter); diff != "" {
		t.Errorf("posting changed (-before +after):\n%s", diff)
	}
	markupAfter, err := postings.Markup(ctx, p.ID)
	if err != nil {
		t.Fatalf("Markup after delete: %v", err)
	}
	if diff := cmp.Diff(interested.After, *markupAfter); diff != "" {
		t.Errorf("posting markup changed (-before +after):\n%s", diff)
	}

	again, err := postings.ApplyTo(ctx, p.ID)
	if err != nil {
		t.Fatalf("ApplyTo after delete: %v", err)
	}
	history, err := applications.History(ctx, again.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 || history[0].Status != application.Started {
		t.Errorf("history of the new application = %+v, want just its own started row", history)
	}
}

// A posting whose application was deleted can start a fresh one; the
// posting leads to that one alone, and changing it leaves the deleted one
// as it was.
func TestPostingService_Application_AfterDeleteAndRestart_IsTheLiveOne(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	postings := s.Postings
	applications := s.Applications
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
	first := testutil.MustApplyTo(t, s, p.ID)
	if _, err := applications.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted, err := applications.GetIncludingDeleted(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetIncludingDeleted: %v", err)
	}
	live := testutil.MustApplyTo(t, s, p.ID)

	got, err := postings.Application(ctx, p.ID)
	if err != nil {
		t.Fatalf("Application: %v", err)
	}
	if got.ID != live.ID {
		t.Fatalf("Application = %d, want the live one %d", got.ID, live.ID)
	}
	if _, err := applications.SetStatus(ctx, got.ID, application.Submitted); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if _, err := applications.SetNotes(ctx, got.ID, "second try"); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	after, err := applications.GetIncludingDeleted(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetIncludingDeleted: %v", err)
	}
	if diff := cmp.Diff(deleted, after); diff != "" {
		t.Errorf("the deleted application changed (-before +after):\n%s", diff)
	}
}

func TestPostingService_Form(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")

	none, err := s.Postings.Form(ctx, p.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Form before any save: err = %v, want store.ErrNotFound", err)
	}
	if none != nil {
		t.Errorf("Form before any save = %+v, want nil", none)
	}

	const formJSON = `{"Questions":[]}`
	if err := s.Postings.SaveForm(ctx, p.ID, formJSON); err != nil {
		t.Fatalf("SaveForm: %v", err)
	}
	got, err := s.Postings.Form(ctx, p.ID)
	if err != nil {
		t.Fatalf("Form: %v", err)
	}
	want := &posting.Form{PostingID: p.ID, JSON: formJSON}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(posting.Form{}, "FetchedAt")); diff != "" {
		t.Errorf("Form (-want +got):\n%s", diff)
	}
	if got.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero, want when the form was saved")
	}
}

// Before is read in the write's transaction, so it's what the last writer
// left, not what the caller last saw.
func TestPostingService_MarkupWrites_ReturnTheMarkupBeforeAndAfter(t *testing.T) {
	t.Parallel()
	writes := []struct {
		name  string
		write func(*service.PostingService, context.Context, int64) (*change.Of[posting.Markup], error)
	}{
		{"SetInterested", (*service.PostingService).SetInterested},
		{"UnmarkInterested", (*service.PostingService).UnmarkInterested},
		{"SetArchived", (*service.PostingService).SetArchived},
		{"Unarchive", (*service.PostingService).Unarchive},
		{"SetNotes", func(s *service.PostingService, ctx context.Context, id int64) (*change.Of[posting.Markup], error) {
			return s.SetNotes(ctx, id, "Asked a recruiter")
		}},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			p := testutil.MustIngestPosting(t, s, "job-1", "Software Engineer")
			if _, err := s.Postings.SetNotes(ctx, p.ID, "Referred by a friend"); err != nil {
				t.Fatalf("SetNotes: %v", err)
			}
			before, err := s.Postings.Markup(ctx, p.ID)
			if err != nil {
				t.Fatalf("Markup: %v", err)
			}

			got, err := w.write(s.Postings, ctx, p.ID)
			if err != nil {
				t.Fatalf("%s: %v", w.name, err)
			}

			if diff := cmp.Diff(*before, got.Before); diff != "" {
				t.Errorf("Before (-want +got):\n%s", diff)
			}
			after, err := s.Postings.Markup(ctx, p.ID)
			if err != nil {
				t.Fatalf("Markup: %v", err)
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

func TestPostingService_MarkupWrites_ReturnNotFoundForAPostingNotThere(t *testing.T) {
	t.Parallel()
	writes := []struct {
		name  string
		write func(*service.PostingService, context.Context, int64) (*change.Of[posting.Markup], error)
	}{
		{"SetInterested", (*service.PostingService).SetInterested},
		{"UnmarkInterested", (*service.PostingService).UnmarkInterested},
		{"SetArchived", (*service.PostingService).SetArchived},
		{"Unarchive", (*service.PostingService).Unarchive},
		{"SetNotes", func(s *service.PostingService, ctx context.Context, id int64) (*change.Of[posting.Markup], error) {
			return s.SetNotes(ctx, id, "Asked a recruiter")
		}},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)

			got, err := w.write(s.Postings, context.Background(), 999)

			if !errors.Is(err, store.ErrNotFound) {
				t.Errorf("err = %v, want store.ErrNotFound", err)
			}
			if got != nil {
				t.Errorf("change = %+v, want nil", got)
			}
		})
	}
}
