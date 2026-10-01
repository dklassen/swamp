package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func TestSyncCompany_NewPostingNoFilters_Created(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
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

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
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
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

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
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

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
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

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
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

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
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

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

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
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
	if postings[0].Department != "Engineering" {
		t.Fatalf("posting department = %q, want %q", postings[0].Department, "Engineering")
	}
	if postings[0].Location != "Dublin, Ireland" {
		t.Fatalf("posting location = %q, want %q", postings[0].Location, "Dublin, Ireland")
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

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1", result.Created)
	}
}

func TestApplyCompanyFilters_ReplacesFiltersThenSyncs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, company.ID, "department", "Sales"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {
			samplePosting("job-1", "Engineer", "Engineering", "Remote"),
			samplePosting("job-2", "Salesperson", "Sales", "Remote"),
		},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

	result, err := syncer.ApplyCompanyFilters(ctx, company.ID, []string{"Engineering"}, nil)
	if err != nil {
		t.Fatalf("ApplyCompanyFilters: %v", err)
	}

	// Sync ran under the new filters (Engineering, not the old Sales
	// filter): only the Engineering posting should have been created.
	if result.Created != 1 {
		t.Fatalf("result.Created = %d, want 1 (only the Engineering posting matches the new filter)", result.Created)
	}

	saved, err := s.ListCompanyFilters(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 1 || saved[0].Field != "department" || saved[0].Value != "Engineering" {
		t.Fatalf("saved filters = %+v, want one department=Engineering filter (old Sales filter replaced)", saved)
	}
}

func TestApplyCompanyFilters_NoDepartmentsOrLocations_ClearsFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if _, err := s.CreateCompanyFilter(ctx, company.ID, "department", "Sales"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

	if _, err := syncer.ApplyCompanyFilters(ctx, company.ID, nil, nil); err != nil {
		t.Fatalf("ApplyCompanyFilters: %v", err)
	}

	saved, err := s.ListCompanyFilters(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("saved filters = %+v, want empty", saved)
	}
}

// closeOnlyPosting syncs one posting into existence, then syncs again
// with an empty fetch so that posting closes. It returns the posting's
// ID and the closing sync's result -- the setup every
// application-closing test below needs (see #105).
func closeOnlyPosting(t *testing.T, s *store.Store, applyBeforeClose func(postingID int64)) (int64, Result) {
	t.Helper()
	ctx := context.Background()

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}

	if applyBeforeClose != nil {
		applyBeforeClose(postings[0].ID)
	}

	fetcher.postings["acme"] = nil
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("closing SyncCompany: %v", err)
	}
	return postings[0].ID, result
}

func TestSyncCompany_PostingCloses_EarlyStageApplicationClosedToo(t *testing.T) {
	ctx := context.Background()

	for _, start := range []store.ApplicationStatus{
		store.ApplicationStatusStarted,
	} {
		t.Run(start.String(), func(t *testing.T) {
			s := newTestStore(t)
			postingID, result := closeOnlyPosting(t, s, func(postingID int64) {
				if _, err := s.CreateApplication(ctx, postingID); err != nil {
					t.Fatalf("CreateApplication: %v", err)
				}
				if _, err := s.UpdateApplicationStatus(ctx, postingID, start); err != nil {
					t.Fatalf("UpdateApplicationStatus: %v", err)
				}
			})

			application, err := s.GetApplication(ctx, postingID)
			if err != nil {
				t.Fatalf("GetApplication: %v", err)
			}
			if application.Status != store.ApplicationStatusPostingClosed {
				t.Errorf("application status = %s, want %s -- an application still at %s when its posting is taken down is over", application.Status, store.ApplicationStatusPostingClosed, start)
			}
			if result.ApplicationsClosed != 1 {
				t.Errorf("result.ApplicationsClosed = %d, want 1", result.ApplicationsClosed)
			}
		})
	}
}

// TestSyncCompany_PostingCloses_LiveApplicationLeftAlone is the guard on
// the whole feature: a company pulling its listing while you're mid
// process is normal, and the syncer must not overwrite a status the user
// set and can't get back (see #105). A submitted application is
// live too: companies often pull a listing once they have enough
// candidates and keep reviewing the ones who applied (#174).
func TestSyncCompany_PostingCloses_LiveApplicationLeftAlone(t *testing.T) {
	ctx := context.Background()

	for _, start := range []store.ApplicationStatus{
		store.ApplicationStatusSubmitted,
		store.ApplicationStatusInterviewing,
		store.ApplicationStatusOfferReceived,
		store.ApplicationStatusOfferAccepted,
	} {
		t.Run(start.String(), func(t *testing.T) {
			s := newTestStore(t)
			postingID, result := closeOnlyPosting(t, s, func(postingID int64) {
				if _, err := s.CreateApplication(ctx, postingID); err != nil {
					t.Fatalf("CreateApplication: %v", err)
				}
				if _, err := s.UpdateApplicationStatus(ctx, postingID, start); err != nil {
					t.Fatalf("UpdateApplicationStatus: %v", err)
				}
			})

			application, err := s.GetApplication(ctx, postingID)
			if err != nil {
				t.Fatalf("GetApplication: %v", err)
			}
			if application.Status != start {
				t.Errorf("application status = %s, want it left at %s -- a listing coming down doesn't end a process already underway", application.Status, start)
			}
			if result.ApplicationsClosed != 0 {
				t.Errorf("result.ApplicationsClosed = %d, want 0", result.ApplicationsClosed)
			}
		})
	}
}

func TestSyncCompany_PostingCloses_NoApplication_IsNotAnError(t *testing.T) {
	s := newTestStore(t)

	postingID, result := closeOnlyPosting(t, s, nil)

	if result.Closed != 1 {
		t.Errorf("result.Closed = %d, want 1 (the posting still closes)", result.Closed)
	}
	if result.ApplicationsClosed != 0 {
		t.Errorf("result.ApplicationsClosed = %d, want 0 (there was no application)", result.ApplicationsClosed)
	}
	if _, err := s.GetApplication(context.Background(), postingID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetApplication err = %v, want ErrNotFound -- closing a posting must not conjure an application", err)
	}
}

// TestSyncCompany_PostingCloses_WithdrawnApplicationLeftAlone keeps the
// two close paths from colliding: withdrawing is the user's own record
// of their decision, and a listing later coming down must not rewrite it
// as posting_closed, which would attribute the ending to the company
// instead of to them.
func TestSyncCompany_PostingCloses_WithdrawnApplicationLeftAlone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	postingID, result := closeOnlyPosting(t, s, func(postingID int64) {
		if _, err := s.CreateApplication(ctx, postingID); err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		if _, err := s.UpdateApplicationStatus(ctx, postingID, store.ApplicationStatusWithdrawn); err != nil {
			t.Fatalf("UpdateApplicationStatus: %v", err)
		}
	})

	application, err := s.GetApplication(ctx, postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if application.Status != store.ApplicationStatusWithdrawn {
		t.Errorf("application status = %s, want it left at withdrawn -- the user ended this one, not the company", application.Status)
	}
	if result.ApplicationsClosed != 0 {
		t.Errorf("result.ApplicationsClosed = %d, want 0", result.ApplicationsClosed)
	}
}

// TestSyncCompany_PostingReappears_ApplicationRestored: a board that
// briefly drops a listing no longer ends its started application for good
// (#174). The sync that reopens the posting puts the application back.
func TestSyncCompany_PostingReappears_ApplicationRestored(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	listed := []jobboard.Posting{samplePosting("job-1", "Engineer", "Engineering", "Remote")}
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": listed}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	postingID := postings[0].ID
	if _, err := s.CreateApplication(ctx, postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
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

	application, err := s.GetApplication(ctx, postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if application.Status != store.ApplicationStatusStarted {
		t.Errorf("application status = %s, want %s restored", application.Status, store.ApplicationStatusStarted)
	}
	if result.Reopened != 1 || result.ApplicationsRestored != 1 {
		t.Errorf("Reopened = %d, ApplicationsRestored = %d, want 1 and 1", result.Reopened, result.ApplicationsRestored)
	}
}

// TestSyncCompany_PostingCloses_ApplicationUpdateFails_NothingHalfClosed
// pins that closing a posting and closing its application are one change
// (#147). Before, the posting was closed and committed first; if the
// application update then failed (SQLITE_BUSY, a closed database), the
// next sync skipped the no-longer-open posting and the application stayed
// at application_started for good.
func TestSyncCompany_PostingCloses_ApplicationUpdateFails_NothingHalfClosed(t *testing.T) {
	ctx := context.Background()
	s, sqlDB := newTestStoreDB(t)

	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	postingID := postings[0].ID
	if _, err := s.CreateApplication(ctx, postingID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, `CREATE TRIGGER fail_application_update BEFORE UPDATE ON applications
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	fetcher.postings["acme"] = nil
	if _, err := syncer.SyncCompany(ctx, company.ID); err == nil {
		t.Fatal("SyncCompany with a failing application update: want an error, got nil")
	}

	posting, err := s.GetPosting(ctx, postingID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if posting.ListingStatus != "open" {
		t.Errorf("after the failed sync, posting listing_status = %q, want still open -- its application wasn't closed, so neither is it", posting.ListingStatus)
	}
	history, err := s.ListPostingHistory(ctx, postingID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("after the failed sync, %d history rows, want 0 -- the close it would record didn't happen", len(history))
	}

	if _, err := sqlDB.ExecContext(ctx, `DROP TRIGGER fail_application_update`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany after the failure cleared: %v", err)
	}
	application, err := s.GetApplication(ctx, postingID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if application.Status != store.ApplicationStatusPostingClosed {
		t.Errorf("after the next clean sync, application status = %s, want %s", application.Status, store.ApplicationStatusPostingClosed)
	}
	if result.Closed != 1 || result.ApplicationsClosed != 1 {
		t.Errorf("next clean sync: Closed = %d, ApplicationsClosed = %d, want 1 and 1", result.Closed, result.ApplicationsClosed)
	}
}

// TestSyncCompany_OverlappingSyncsClosePostings_EachCloseRecordedOnce:
// two syncs of one company that both fetched before either wrote both
// find the same postings gone. Only the first close of each posting may
// count or be recorded; the second finds it already closed (#147).
func TestSyncCompany_OverlappingSyncsClosePostings_EachCloseRecordedOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	var onBoard []jobboard.Posting
	for _, id := range []string{"job-1", "job-2", "job-3", "job-4", "job-5"} {
		onBoard = append(onBoard, samplePosting(id, "Engineer", "Engineering", "Remote"))
	}
	seed := New(s, map[string]PostingFetcher{"ashby": &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": onBoard}}}, DefaultConfig())
	if _, err := seed.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}

	results := syncOverlapping(t, s, company.ID, 2, nil)

	closedCount := 0
	for _, r := range results {
		closedCount += r.Closed
	}
	if closedCount != len(onBoard) {
		t.Errorf("Closed summed over both runs = %d, want %d -- each posting closes once", closedCount, len(onBoard))
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	for _, p := range postings {
		history, err := s.ListPostingHistory(ctx, p.ID)
		if err != nil {
			t.Fatalf("ListPostingHistory: %v", err)
		}
		if len(history) != 1 || history[0].ChangeType != "closed" {
			t.Errorf("posting %s history = %d rows, want exactly one \"closed\" row", p.SourceID, len(history))
		}
	}
}

// TestSyncCompany_OverlappingSyncsCreatePosting_CountedOnce: two syncs
// that both fetched before either wrote both see a posting that isn't in
// the database yet. Only the run that actually creates it may count it
// (#148); the other finds it already there, unchanged.
func TestSyncCompany_OverlappingSyncsCreatePosting_CountedOnce(t *testing.T) {
	s := newTestStore(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")

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

// TestSyncCompany_OverlappingSyncsReopenChangedPosting_EachChangeRecordedOnce:
// a closed posting comes back with new content, and two syncs that both
// fetched before either wrote see it. Each change -- the new content and
// the reopen -- is recorded and counted once (#148).
func TestSyncCompany_OverlappingSyncsReopenChangedPosting_EachChangeRecordedOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	original := samplePosting("job-1", "Engineer", "Engineering", "Remote")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {original}}}
	seed := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	if _, err := seed.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	fetcher.postings["acme"] = nil
	if _, err := seed.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("closing SyncCompany: %v", err)
	}

	changed := original
	changed.Title = "Senior Engineer"
	results := syncOverlapping(t, s, company.ID, 2, []jobboard.Posting{changed})

	updated, reopened := 0, 0
	for _, r := range results {
		updated += r.Updated
		reopened += r.Reopened
	}
	if updated != 1 || reopened != 1 {
		t.Errorf("summed over both runs: Updated = %d, Reopened = %d, want 1 and 1", updated, reopened)
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	history, err := s.ListPostingHistory(ctx, postings[0].ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	var got []string
	for _, h := range history {
		got = append(got, h.ChangeType)
	}
	want := []string{"closed", "content_updated", "reopened"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("history change types mismatch (-want +got):\n%s", diff)
	}
}

// TestSyncCompany_ContentUpdateFails_NoHistoryUntilItHappens: a
// "content_updated" history row is written with the update it records, so
// an update that fails leaves no history behind, and the next sync that
// makes the change records it exactly once (#148). Before, the history
// row was committed first, and each failed attempt added another.
func TestSyncCompany_ContentUpdateFails_NoHistoryUntilItHappens(t *testing.T) {
	ctx := context.Background()
	s, sqlDB := newTestStoreDB(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	postingID := postings[0].ID

	if _, err := sqlDB.ExecContext(ctx, `CREATE TRIGGER fail_posting_update BEFORE UPDATE ON postings
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	fetcher.postings["acme"] = []jobboard.Posting{samplePosting("job-1", "Senior Engineer", "Engineering", "Remote")}
	if _, err := syncer.SyncCompany(ctx, company.ID); err == nil {
		t.Fatal("SyncCompany with a failing posting update: want an error, got nil")
	}
	history, err := s.ListPostingHistory(ctx, postingID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("after the failed update, %d history rows, want 0 -- the change it would record didn't happen", len(history))
	}

	if _, err := sqlDB.ExecContext(ctx, `DROP TRIGGER fail_posting_update`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("SyncCompany after the failure cleared: %v", err)
	}
	history, err = s.ListPostingHistory(ctx, postingID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if len(history) != 1 || history[0].ChangeType != "content_updated" {
		t.Errorf("after the next clean sync, history = %d rows, want exactly one content_updated", len(history))
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

			s := newTestStore(t)
			ctx := context.Background()

			company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			fetcher := &fakeFetcher{
				postings: map[string][]jobboard.Posting{"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")}},
				err:      tc.fetchErr,
			}
			syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

			before := time.Now().Add(-2 * time.Second)
			_, err := syncer.SyncCompany(ctx, company.ID)
			if (err == nil) != tc.wantFetched {
				t.Fatalf("SyncCompany error = %v, want error: %v", err, !tc.wantFetched)
			}

			got, err := s.GetCompany(ctx, company.ID)
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

// TestSyncCompany_RecordsLastSeenAtForEveryListedPosting: last_seen_at
// is the last time a sync saw the posting on its board (#176). Every
// stored posting the fetch returns advances -- unchanged ones, and ones
// the company's filters now leave out but the board still lists -- with
// no history row and no update counted. A posting the board dropped
// keeps the time it was last seen.
func TestSyncCompany_RecordsLastSeenAtForEveryListedPosting(t *testing.T) {
	ctx := context.Background()
	s, sqlDB := newTestStoreDB(t)
	company := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	unchanged := samplePosting("job-1", "Engineer", "Engineering", "Remote")
	filteredOut := samplePosting("job-2", "Designer", "Design", "Remote")
	dropped := samplePosting("job-3", "Writer", "Marketing", "Remote")
	fetcher := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {unchanged, filteredOut, dropped},
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	if _, err := syncer.SyncCompany(ctx, company.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	longAgo := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := sqlDB.ExecContext(ctx, `UPDATE postings SET last_seen_at = ?`, longAgo); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
	if err := s.ReplaceCompanyFilters(ctx, company.ID, []store.CompanyFilter{{Field: filter.FieldDepartment, Value: "Engineering"}}); err != nil {
		t.Fatalf("ReplaceCompanyFilters: %v", err)
	}

	fetcher.postings["acme"] = []jobboard.Posting{unchanged, filteredOut}
	before := time.Now().Add(-2 * time.Second)
	result, err := syncer.SyncCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.Updated != 0 {
		t.Errorf("result.Updated = %d, want 0: being seen isn't a content change", result.Updated)
	}

	postings, err := s.ListPostingsByCompany(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	for _, p := range postings {
		listed := p.SourceID != dropped.SourceID
		switch {
		case listed && p.LastSeenAt.Before(before):
			t.Errorf("%s: LastSeenAt = %v, want at or after %v: the board still lists it", p.SourceID, p.LastSeenAt, before)
		case !listed && !p.LastSeenAt.Equal(longAgo):
			t.Errorf("%s: LastSeenAt = %v, want %v unchanged: the board dropped it", p.SourceID, p.LastSeenAt, longAgo)
		}
		history, err := s.ListPostingHistory(ctx, p.ID)
		if err != nil {
			t.Fatalf("ListPostingHistory: %v", err)
		}
		for _, h := range history {
			if h.ChangeType == "content_updated" {
				t.Errorf("%s: a content_updated history row, want none: being seen isn't a change", p.SourceID)
			}
		}
	}
}
