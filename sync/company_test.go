package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

func TestSyncCompany_NewPostingNoFilters_Created(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}

	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1", result.Created)
	}

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
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

// Filters only decide what's shown: a posting they leave out is still
// stored, so a stale filter can hide it but never lose it.
func TestSyncCompany_PostingDoesNotMatchFilters_Created(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	addCompanyFilter(t, s, company.ID, "department", "Sales")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}

	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1", result.Created)
	}

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 || postings[0].Department != "Engineering" {
		t.Fatalf("postings = %+v, want the one Engineering posting", postings)
	}
}

// A company renaming a department mustn't freeze its stored postings at
// their old content just because the filter names the old value.
func TestSyncCompany_DepartmentRenamedPastFilter_ContentUpdated(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	addCompanyFilter(t, s, company.ID, "department", "Engineering")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}

	fetcher.postings["acme"] = []jobboard.Posting{samplePosting("job-1", "Engineer", "Eng", "Remote")}
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result.Updated = %d, want 1", result.Updated)
	}

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 || postings[0].Department != "Eng" || postings[0].ListingStatus != "open" {
		t.Fatalf("postings = %+v, want the one posting, open, with department Eng", postings)
	}
	history, err := s.Postings.History(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 1 || history[0].ChangeType != "content_updated" {
		t.Fatalf("history = %+v, want one content_updated row", history)
	}
}

func TestSyncCompany_ExistingPostingContentChanged_UpdatedWithHistory(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

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

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].Title != "Senior Engineer" {
		t.Fatalf("posting title = %q, want %q", postings[0].Title, "Senior Engineer")
	}

	history, err := s.Postings.History(ctx, postings[0].ID)
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
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

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

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	history, err := s.Postings.History(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("got %d history entries, want 0", len(history))
	}
}

func TestSyncCompany_PostingDisappearsFromFetch_ClosedWithHistory(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

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

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].ListingStatus != "closed" {
		t.Fatalf("posting ListingStatus = %q, want %q", postings[0].ListingStatus, "closed")
	}

	history, err := s.Postings.History(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 1 || history[0].ChangeType != "closed" {
		t.Fatalf("history = %+v, want one 'closed' entry", history)
	}
}

func TestSyncCompany_ClosedPostingReappears_ReopenedWithHistory(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := samplePosting("job-1", "Engineer", "Engineering", "Remote")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {posting}}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

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

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 || postings[0].ListingStatus != "open" {
		t.Fatalf("postings = %+v, want one open posting", postings)
	}

	history, err := s.Postings.History(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 2 || history[1].ChangeType != "reopened" {
		t.Fatalf("history = %+v, want [closed, reopened]", history)
	}
}

func TestSyncCompany_UnsupportedSource_ReturnsError(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Lever, "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

	_, err := syncer.SyncCompany(ctx, company.ID)
	if err == nil {
		t.Fatal("SyncCompany: expected error for unsupported source \"lever\", got nil")
	}
}

func TestSyncCompany_FetchedPostingHasWhitespace_SavedTrimmed(t *testing.T) {
	s := testutil.NewTestApp(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	padded := samplePosting("job-1", " Engineer ", " Engineering", "Dublin, Ireland ")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {padded}}}

	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}

	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}
	if postings[0].Title != "Engineer" {
		t.Fatalf("posting title = %q, want %q", postings[0].Title, "Engineer")
	}
	if postings[0].Department != "Engineering" {
		t.Fatalf("posting department = %q, want %q", postings[0].Department, "Engineering")
	}
	if postings[0].Location != "Dublin, Ireland" {
		t.Fatalf("posting location = %q, want %q", postings[0].Location, "Dublin, Ireland")
	}
}

// TestSyncCompany_PostingReappears_ApplicationRestored: a board that
// briefly drops a listing no longer ends its started application for good
// . The sync that reopens the posting puts the application back.
func TestSyncCompany_PostingReappears_ApplicationRestored(t *testing.T) {
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	listed := []jobboard.Posting{samplePosting("job-1", "Engineer", "Engineering", "Remote")}
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": listed}}
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.Postings.ListByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	postingID := postings[0].ID
	if _, err := createApplication(ctx, s, postingID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}

	fetcher.postings["acme"] = nil
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("closing SyncCompany: %v", err)
	}
	fetcher.postings["acme"] = listed
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("reopening SyncCompany: %v", err)
	}

	app, err := s.Postings.Application(ctx, postingID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if app.Status != application.Started {
		t.Errorf("application status = %s, want %s restored", app.Status, application.Started)
	}
	if result.Reopened != 1 {
		t.Errorf("Reopened = %d, want 1", result.Reopened)
	}
}

// TestSyncCompany_OverlappingSyncsCreatePosting_CountedOnce: two syncs
// that both fetched before either wrote both see a posting that isn't in
// the database yet. Only the run that actually creates it may count it
// ; the other finds it already there, unchanged.
func TestSyncCompany_OverlappingSyncsCreatePosting_CountedOnce(t *testing.T) {
	s := testutil.NewTestApp(t)
	company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")

	results := syncOverlapping(t, s, company.ID, 2, []jobboard.Posting{
		samplePosting("job-1", "Engineer", "Engineering", "Remote"),
	})

	created, updated := 0, 0
	for _, r := range results {
		created += r.Created
		updated += r.Updated
	}
	if created != 1 || updated != 0 {
		t.Errorf("summed over both runs: Created = %d, Updated = %d, want 1 and 0 -- one posting, created once, never changed", created, updated)
	}
}

// A successful sync records when the company was fetched; a failed fetch
// leaves it alone, so a stale "last fetched" is a visible sign of trouble.
func TestSyncCompany_RecordsLastFetchedAtOnlyOnSuccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		fetchErr    error
		wantFetched bool
	}{
		{"fetch succeeds", nil, true},
		{"fetch fails", errors.New("board unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := testutil.NewTestApp(t)
			ctx := context.Background()

			company := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			fetcher := &fakeFetcher{
				postings: map[string][]jobboard.Posting{"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")}},
				err:      tc.fetchErr,
			}
			syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())

			before := time.Now().Add(-2 * time.Second)
			_, err := syncer.SyncCompany(ctx, company.ID)
			if (err == nil) != tc.wantFetched {
				t.Fatalf("SyncCompany error = %v, want error: %v", err, !tc.wantFetched)
			}

			got, err := s.Companies.Get(ctx, company.ID)
			if err != nil {
				t.Fatalf("GetCompany: %v", err)
			}
			if tc.wantFetched {
				if got.LastFetchedAt.Before(before) {
					t.Errorf("LastFetchedAt = %v, want a time at or after %v", got.LastFetchedAt, before)
				}
			} else if !got.LastFetchedAt.IsZero() {
				t.Errorf("LastFetchedAt = %v, want zero (never fetched)", got.LastFetchedAt)
			}
		})
	}
}
