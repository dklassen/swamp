package sync

import (
	"context"
	"testing"

	"github.com/dklassen/swamp/jobboard"
)

func TestSyncCompany_NewPostingNoFilters_Created(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1", result.Created)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].Title != "Engineer" {
		t.Fatalf("posting title = %q, want %q", postings[0].Title, "Engineer")
	}
}

func TestSyncCompany_PostingDoesNotMatchFilters_NotCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, company.ID, "department", "Sales"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	if result.Created != 0 {
		t.Fatalf("result.Created = %d, want 0", result.Created)
	}
	if result.Fetched != 1 {
		t.Fatalf("result.Fetched = %d, want 1", result.Fetched)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 0 {
		t.Fatalf("got %d postings, want 0", len(postings))
	}
}

func TestSyncCompany_ExistingPostingContentChanged_UpdatedWithHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}

	fetcher.postings["acme"] = []jobboard.Posting{
		samplePosting("job-1", "Senior Engineer", "Engineering", "Remote"),
	}

	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("second SyncCompany: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result.Updated = %d, want 1", result.Updated)
	}
	if result.Created != 0 {
		t.Fatalf("result.Created = %d, want 0", result.Created)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].Title != "Senior Engineer" {
		t.Fatalf("posting title = %q, want %q", postings[0].Title, "Senior Engineer")
	}

	history, err := s.ListPostingHistory(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("got %d history entries, want 1", len(history))
	}
	if history[0].ChangeType != "content_updated" {
		t.Fatalf("history[0].ChangeType = %q, want %q", history[0].ChangeType, "content_updated")
	}
}

func TestSyncCompany_ExistingPostingUnchanged_NoUpdateNoHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}

	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("second SyncCompany: %v", err)
	}
	if result.Updated != 0 {
		t.Fatalf("result.Updated = %d, want 0", result.Updated)
	}
	if result.Created != 0 {
		t.Fatalf("result.Created = %d, want 0", result.Created)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	history, err := s.ListPostingHistory(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("got %d history entries, want 0", len(history))
	}
}

func TestSyncCompany_PostingDisappearsFromFetch_ClosedWithHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}

	fetcher.postings["acme"] = nil

	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("second SyncCompany: %v", err)
	}
	if result.Closed != 1 {
		t.Fatalf("result.Closed = %d, want 1", result.Closed)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].ListingStatus != "closed" {
		t.Fatalf("posting ListingStatus = %q, want %q", postings[0].ListingStatus, "closed")
	}

	history, err := s.ListPostingHistory(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 1 || history[0].ChangeType != "closed" {
		t.Fatalf("history = %+v, want one 'closed' entry", history)
	}
}

func TestSyncCompany_ClosedPostingReappears_ReopenedWithHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := samplePosting("job-1", "Engineer", "Engineering", "Remote")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {posting}}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	fetcher.postings["acme"] = nil
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("closing SyncCompany: %v", err)
	}

	fetcher.postings["acme"] = []jobboard.Posting{posting}
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("reopening SyncCompany: %v", err)
	}
	if result.Reopened != 1 {
		t.Fatalf("result.Reopened = %d, want 1", result.Reopened)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 || postings[0].ListingStatus != "open" {
		t.Fatalf("postings = %+v, want one open posting", postings)
	}

	history, err := s.ListPostingHistory(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 2 || history[1].ChangeType != "reopened" {
		t.Fatalf("history = %+v, want [closed, reopened]", history)
	}
}

func TestSyncCompany_UnsupportedSource_ReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "lever", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	_, err := syncer.SyncCompany(ctx, company.ID)
	if err == nil {
		t.Fatal("SyncCompany: expected error for unsupported source \"lever\", got nil")
	}
}

func TestSyncCompany_FetchedPostingHasWhitespace_SavedTrimmed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	padded := samplePosting("job-1", " Engineer ", " Engineering", "Dublin, Ireland ")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {padded}}}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].Title != "Engineer" {
		t.Fatalf("posting title = %q, want %q", postings[0].Title, "Engineer")
	}
	if postings[0].Department == nil || *postings[0].Department != "Engineering" {
		t.Fatalf("posting department = %v, want %q", postings[0].Department, "Engineering")
	}
	if postings[0].Location == nil || *postings[0].Location != "Dublin, Ireland" {
		t.Fatalf("posting location = %v, want %q", postings[0].Location, "Dublin, Ireland")
	}
}

// This is the concrete bug the whitespace issue caused: filter.Match does
// an exact (case-insensitive) comparison, so a clean filter value like
// "Canada" silently failed to match a fetched posting location of
// "Canada " before fetched postings were sanitized.
func TestSyncCompany_FilterValueMatchesTrimmedLocation_PostingCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, company.ID, "location", "Canada"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	padded := samplePosting("job-1", "Engineer", "Engineering", "Canada ")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {padded}}}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1", result.Created)
	}
}
