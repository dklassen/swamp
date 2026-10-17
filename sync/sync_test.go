package sync

import (
	"context"
	gosync "sync"
	"testing"
	"time"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

func mustCreateCompany(t *testing.T, s testutil.TestApp, name, source, sourceRef string) company.Company {
	t.Helper()
	c, err := s.Companies.Add(context.Background(), name, source, sourceRef)
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

// syncOverlapping runs `runs` syncs of companyID's write phase at once,
// all held at a barrierFetcher until every one has fetched postings, and
// returns their results in no particular order.
//
// It calls syncHeld, underneath SyncCompany's sync lease: the lease
// stops overlapping syncs, but the writes must stay safe if they overlap
// anyway -- e.g. a lease that expired while its sync was still running
// and was taken over.
func syncOverlapping(t *testing.T, s testutil.TestApp, companyID int64, runs int, postings []jobboard.Posting) []Result {
	t.Helper()
	fetcher := newBarrierFetcher(runs, postings)
	syncer := New(s.Companies, s.Postings, map[string]PostingFetcher{jobboard.Ashby: fetcher}, DefaultConfig())
	company, err := s.Companies.Get(context.Background(), companyID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	results := make([]Result, runs)
	errs := make([]error, runs)
	var done gosync.WaitGroup
	for i := range runs {
		done.Add(1)
		go func() {
			defer done.Done()
			results[i], errs[i] = syncer.syncHeld(context.Background(), company, fetcher, Result{CompanyID: companyID})
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

func addCompanyFilter(t *testing.T, s testutil.TestApp, companyID int64, field, value string) {
	t.Helper()
	ctx := context.Background()
	existing, err := s.Companies.Filters(ctx, companyID)
	if err == nil {
		err = s.Companies.SetFilters(ctx, companyID, append(existing, filter.Filter{Field: field, Value: value}))
	}
	if err != nil {
		t.Fatalf("add company filter %s=%s: %v", field, value, err)
	}
}

func createApplication(ctx context.Context, s testutil.TestApp, postingID int64) (application.Application, error) {
	return s.Postings.ApplyTo(ctx, postingID)
}

// updateApplicationStatus sets postingID's application to status, as the
// user would.
func updateApplicationStatus(ctx context.Context, s testutil.TestApp, postingID int64, status application.Status) (application.Application, error) {
	app, err := s.Postings.Application(ctx, postingID)
	if err != nil {
		return application.Application{}, err
	}
	return s.Applications.SetStatus(ctx, app.ID, status)
}
