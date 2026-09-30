package sync

import (
	"context"
	"database/sql"
	gosync "sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, _ := newTestStoreDB(t)
	return s
}

// newTestStoreDB is newTestStore that also returns the underlying
// *sql.DB, for tests that need to reach past store -- e.g. to install a
// trigger that makes one write fail.
func newTestStoreDB(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()

	// store.Open, not sql.Open, so tests run with the same connection
	// settings as swamp -- overlapping-sync tests depend on its busy
	// timeout and immediate transactions.
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

	return store.New(sqlDB), sqlDB
}

func mustCreateCompany(t *testing.T, s *store.Store, name, source, sourceRef string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, source, sourceRef)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

// fakeFetcher is a PostingFetcher whose FetchPostings return value is
// configured per test, so sync tests never make real HTTP calls.
type fakeFetcher struct {
	postings map[string][]jobboard.Posting // boardSlug -> postings
	err      error
}

func (f *fakeFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.postings[boardSlug], nil
}

// barrierFetcher returns postings to every caller, but only once all
// `callers` fetches have arrived, so that many overlapping SyncCompany
// runs all finish fetching before any of them writes.
type barrierFetcher struct {
	postings []jobboard.Posting
	arrived  *gosync.WaitGroup
}

func newBarrierFetcher(callers int, postings []jobboard.Posting) *barrierFetcher {
	arrived := &gosync.WaitGroup{}
	arrived.Add(callers)
	return &barrierFetcher{postings: postings, arrived: arrived}
}

func (f *barrierFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	f.arrived.Done()
	f.arrived.Wait()
	return f.postings, nil
}

// syncOverlapping runs `runs` SyncCompany calls for companyID at once,
// all held at a barrierFetcher until every one has fetched postings, and
// returns their results in no particular order.
func syncOverlapping(t *testing.T, s *store.Store, companyID int64, runs int, postings []jobboard.Posting) []Result {
	t.Helper()
	syncer := New(s, map[string]PostingFetcher{"ashby": newBarrierFetcher(runs, postings)}, DefaultConfig())
	results := make([]Result, runs)
	errs := make([]error, runs)
	var done gosync.WaitGroup
	for i := range runs {
		done.Add(1)
		go func() {
			defer done.Done()
			results[i], errs[i] = syncer.SyncCompany(context.Background(), companyID)
		}()
	}
	done.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("overlapping SyncCompany: %v", err)
		}
	}
	return results
}

// perBoardFetcher is a PostingFetcher that can fail for specific board
// slugs while succeeding for others, for testing SyncAll's per-company
// error isolation.
type perBoardFetcher struct {
	postings  map[string][]jobboard.Posting
	errBoards map[string]error
}

func (f *perBoardFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	if err, ok := f.errBoards[boardSlug]; ok {
		return nil, err
	}
	return f.postings[boardSlug], nil
}

func samplePosting(sourceID, title, department, location string) jobboard.Posting {
	return jobboard.Posting{
		SourceID:        sourceID,
		Title:           title,
		Department:      department,
		Location:        location,
		EmploymentType:  "FullTime",
		WorkplaceType:   "Remote",
		DescriptionHTML: "<p>desc</p>",
		DescriptionText: "desc",
		JobURL:          "https://jobs.ashbyhq.com/acme/" + sourceID,
		ApplicationURL:  "https://jobs.ashbyhq.com/acme/" + sourceID + "/application",
		PublishedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RawPayload:      []byte(`{"id":"` + sourceID + `"}`),
	}
}
