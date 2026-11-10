package service_test

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
)

func TestDraftingService_Queue_InterestedPosting_IsQueued(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	if _, err := s.Postings.SetInterested(t.Context(), p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}

	got, err := s.Drafting.Queue(t.Context())
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	want := []service.QueuedPosting{{Posting: p, CompanyName: "Acme"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Queue (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Queue_StartedPosting_CarriesItsApplication(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	if _, err := s.Postings.SetInterested(ctx, p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	app := testutil.MustApplyTo(t, s, p.ID)
	noted, err := s.Applications.SetNotes(ctx, app.ID, "referred by a friend")
	if err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	want := []service.QueuedPosting{{Posting: p, CompanyName: "Acme", Application: &noted.After, LatestReviews: map[document.Type]document.Review{}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Queue (-want +got):\n%s", diff)
	}
}

// mustStartDrafting marks a posting interested and starts its application.
func mustStartDrafting(t *testing.T, s testutil.TestApp, postingID int64) application.Application {
	t.Helper()
	if _, err := s.Postings.SetInterested(t.Context(), postingID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	return testutil.MustApplyTo(t, s, postingID)
}

// draft is what mustDraft writes.
const draft = "draft"

// mustDraft writes draft as each of documentTypes.
func mustDraft(t *testing.T, s testutil.TestApp, applicationID int64, documentTypes ...document.Type) {
	t.Helper()
	for _, documentType := range documentTypes {
		if _, err := s.Documents.Write(t.Context(), applicationID, documentType, draft, service.NoDocumentYet, document.SourceWriteDocument); err != nil {
			t.Fatalf("Write %s: %v", documentType, err)
		}
	}
}

func TestDraftingService_Queue_EveryDocumentDrafted_LeavesTheQueue(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := mustStartDrafting(t, s, p.ID)
	mustDraft(t, s, app.ID, document.Types()...)

	got, err := s.Drafting.Queue(t.Context())
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Queue = %+v, want none: every document is drafted", got)
	}
}

func TestDraftingService_Queue_EveryDocumentDraftedButOneFlagged_StaysWithItsReview(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := mustStartDrafting(t, s, p.ID)
	mustDraft(t, s, app.ID, document.Types()...)
	review, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, draft, document.Flagged, "too generic")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	want := []service.QueuedPosting{{Posting: p, CompanyName: "Acme", Application: &app, LatestReviews: map[document.Type]document.Review{document.CoverLetter: *review}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Queue (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Queue_FlagOnAnEarlierDraft_DoesntCount(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := mustStartDrafting(t, s, p.ID)
	mustDraft(t, s, app.ID, document.Types()...)
	if _, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, draft, document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if _, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "revised", document.ContentSHA256(draft), document.SourceWriteDocument); err != nil {
		t.Fatalf("Write (revision): %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Queue = %+v, want none: the flag was on an earlier draft", got)
	}
}

func TestDraftingService_Queue_StartedWithoutInterest_IsQueuedAfterInterested(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "Acme")
	globex := testutil.MustAddCompany(t, s, "Globex")
	started := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
	app := testutil.MustApplyTo(t, s, started.ID)
	interested := testutil.MustIngestPostingFor(t, s, globex.ID, "job-2", "Designer")
	if _, err := s.Postings.SetInterested(ctx, interested.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	want := []service.QueuedPosting{
		{Posting: interested, CompanyName: "Globex"},
		{Posting: started, CompanyName: "Acme", Application: &app, LatestReviews: map[document.Type]document.Review{}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Queue (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Queue_StartedWithoutInterestButArchived_IsLeftOut(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	testutil.MustApplyTo(t, s, p.ID)
	if _, err := s.Postings.SetArchived(ctx, p.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Queue = %+v, want none: the posting is archived", got)
	}
}

func TestDraftingService_Queue_ApplicationAtATerminalStatus_IsLeftOut(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		status application.Status
		want   bool
	}{
		{application.Started, true},
		{application.Submitted, true},
		{application.Interviewing, true},
		{application.Rejected, false},
		{application.OfferReceived, true},
		{application.OfferAccepted, true},
		{application.OfferDeclined, false},
		{application.PostingClosed, false},
		{application.Withdrawn, false},
	} {
		t.Run(tt.status.String(), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s := testutil.NewTestApp(t)
			p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
			app := mustStartDrafting(t, s, p.ID)
			if _, err := s.Applications.SetStatus(ctx, app.ID, tt.status); err != nil {
				t.Fatalf("SetStatus: %v", err)
			}

			got, err := s.Drafting.Queue(ctx)
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			if queued := len(got) == 1; queued != tt.want {
				t.Errorf("queued = %v, want %v", queued, tt.want)
			}
		})
	}
}

// Ordered by company name whatever its case, then by when drafting started.
func TestDraftingService_Queue_StartedWithoutInterest_InCompanyOrder(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	globex := testutil.MustAddCompany(t, s, "globex")
	acme := testutil.MustAddCompany(t, s, "Acme")
	var started []int64
	for _, p := range []struct {
		companyID int64
		sourceID  string
	}{{globex.ID, "job-1"}, {acme.ID, "job-2"}, {globex.ID, "job-3"}} {
		posting := testutil.MustIngestPostingFor(t, s, p.companyID, p.sourceID, "Engineer")
		started = append(started, testutil.MustApplyTo(t, s, posting.ID).ID)
	}

	got, err := s.Drafting.Queue(t.Context())
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	var ids []int64
	for _, q := range got {
		ids = append(ids, q.Application.ID)
	}
	if diff := cmp.Diff([]int64{started[1], started[0], started[2]}, ids); diff != "" {
		t.Errorf("application order (-want +got):\n%s", diff)
	}
}

// A reviewed document that can't be read is an error, not a review that
// no longer counts.
func TestDraftingService_Queue_UnreadableReviewedDocument_IsAnError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	if _, err := s.Postings.SetInterested(ctx, p.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	prepared, err := s.Drafting.Prepare(ctx, p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := s.Documents.Review(ctx, prepared.Application.ID, document.CoverLetter, draft, document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if err := os.Mkdir(prepared.Documents[document.CoverLetter].Path, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if got, err := s.Drafting.Queue(ctx); err == nil {
		t.Fatalf("Queue = %+v, want an error for a cover letter that can't be read", got)
	}
}

func TestDraftingService_Queue_PostingNeitherInterestedNorStarted_IsLeftOut(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	testutil.MustIngestPosting(t, s, "job-1", "Engineer")

	got, err := s.Drafting.Queue(t.Context())
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Queue = %+v, want none", got)
	}
}

func TestDraftingService_Queue_SubmittedWithoutInterest_IsLeftOut(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := testutil.MustApplyTo(t, s, p.ID)
	if _, err := s.Applications.SetStatus(ctx, app.ID, application.Submitted); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Queue = %+v, want none: drafting is over once submitted", got)
	}
}

func TestDraftingService_Queue_OneDocumentDrafted_StaysQueued(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := mustStartDrafting(t, s, p.ID)
	mustDraft(t, s, app.ID, document.CoverLetter)

	got, err := s.Drafting.Queue(t.Context())
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Queue = %+v, want the posting: its resume isn't drafted", got)
	}
}

func TestDraftingService_Queue_ReviewOfAnEarlierDraft_IsLeftOut(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	app := mustStartDrafting(t, s, p.ID)
	mustDraft(t, s, app.ID, document.CoverLetter)
	if _, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, draft, document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if _, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "revised", document.ContentSHA256(draft), document.SourceWriteDocument); err != nil {
		t.Fatalf("Write (revision): %v", err)
	}

	got, err := s.Drafting.Queue(ctx)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Queue = %+v, want the posting: its resume isn't drafted", got)
	}
	if diff := cmp.Diff(map[document.Type]document.Review{}, got[0].LatestReviews); diff != "" {
		t.Errorf("LatestReviews (-want +got):\n%s", diff)
	}
}

func TestDraftingService_Queue_InterestedPostingTheCompanyListHides_IsLeftOut(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		hide func(t *testing.T, s testutil.TestApp, companyID, postingID int64)
	}{
		{"archived", func(t *testing.T, s testutil.TestApp, _, postingID int64) {
			t.Helper()
			if _, err := s.Postings.SetArchived(t.Context(), postingID); err != nil {
				t.Fatalf("SetArchived: %v", err)
			}
		}},
		{"closed", func(t *testing.T, s testutil.TestApp, _, postingID int64) {
			t.Helper()
			if err := s.Postings.Close(t.Context(), postingID, nil); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}},
		{"company deleted", func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			t.Helper()
			if _, err := s.Companies.Delete(t.Context(), companyID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}},
		{"filtered out", func(t *testing.T, s testutil.TestApp, companyID, _ int64) {
			t.Helper()
			if err := s.Companies.SetFilters(t.Context(), companyID, []filter.Filter{{Field: filter.FieldLocation, Value: "Nowhere"}}); err != nil {
				t.Fatalf("SetFilters: %v", err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := testutil.MustAddCompany(t, s, "Acme")
			globex := testutil.MustAddCompany(t, s, "Globex")
			hidden := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
			shown := testutil.MustIngestPostingFor(t, s, globex.ID, "job-2", "Engineer")
			for _, id := range []int64{hidden.ID, shown.ID} {
				if _, err := s.Postings.SetInterested(t.Context(), id); err != nil {
					t.Fatalf("SetInterested: %v", err)
				}
			}
			tt.hide(t, s, acme.ID, hidden.ID)

			got, err := s.Drafting.Queue(t.Context())
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			var ids []int64
			for _, q := range got {
				ids = append(ids, q.Posting.ID)
			}
			if diff := cmp.Diff([]int64{shown.ID}, ids); diff != "" {
				t.Errorf("queued postings (-want +got):\n%s", diff)
			}
		})
	}
}
