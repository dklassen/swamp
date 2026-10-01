package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestListPostingListings_EveryPostingWithItsMarkupAndApplication: the
// listing behind the agent's search_postings (#215) covers every posting,
// open or closed, interested or not, with or without an application, at
// any status -- except those of a company the user deleted.
func TestListPostingListings_EveryPostingWithItsMarkupAndApplication(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	plain, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID, Source: "ashby", SourceID: "job-1",
		IngestedFields: IngestedFields{
			Title: "Designer", Department: "Design", Location: "Toronto", WorkplaceType: "Remote",
			ApplicationURL: "https://jobs.example/1/apply", DescriptionText: "long description", RawPayload: "{}",
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	applied := mustUpsertPosting(t, s, acme.ID, "job-2", "Engineer")
	closed := mustUpsertPosting(t, s, acme.ID, "job-3", "Writer")
	gone := mustCreateCompany(t, s, "Gone", "ashby", "gone")
	mustUpsertPosting(t, s, gone.ID, "job-4", "Deleted company's posting")

	if _, err := s.SetPostingInterested(ctx, applied.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	application := mustCreateApplication(t, s, applied.ID)
	mustUpdateApplicationStatus(t, s, applied.ID, ApplicationStatusSubmitted)
	if _, err := s.UpdateApplicationNotes(ctx, applied.ID, "heard back"); err != nil {
		t.Fatalf("UpdateApplicationNotes: %v", err)
	}
	if _, err := s.SetPostingArchived(ctx, closed.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if err := s.SoftDeleteCompany(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	got, err := s.ListPostingListings(ctx)
	if err != nil {
		t.Fatalf("ListPostingListings: %v", err)
	}
	want := []PostingListing{
		{
			ID: plain.ID, Title: "Designer", Department: "Design", Location: "Toronto", WorkplaceType: "Remote",
			ApplicationURL: "https://jobs.example/1/apply", ListingStatus: "open", CompanyName: "Acme",
		},
		{
			ID: applied.ID, Title: "Engineer", ListingStatus: "open", CompanyName: "Acme", Interested: true,
			ApplicationID: application.ID, ApplicationStatus: ApplicationStatusSubmitted, ApplicationNotes: "heard back",
		},
		{ID: closed.ID, Title: "Writer", ListingStatus: "closed", CompanyName: "Acme", Archived: true},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListPostingListings mismatch (-want +got):\n%s", diff)
	}
}
