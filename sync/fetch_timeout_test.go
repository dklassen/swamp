package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dklassen/swamp/jobboard"
)

// hangingFetcher is a PostingFetcher for a board that accepts the request
// but never answers: it blocks until its context is done. The safety
// limit only stops a test hanging for ever when nothing cancels the
// fetch; returning errNeverCancelled marks that as a failure.
type hangingFetcher struct{}

var errNeverCancelled = errors.New("fetch was never cancelled")

func (hangingFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, errNeverCancelled
	}
}

// TestSyncer_FetchesTimeOut checks every board fetch Syncer makes gives up
// after Config.FetchTimeout, rather than waiting on a hung board for ever
// -- the job board clients' http.DefaultClient has no timeout of its own
// (issue #142).
func TestSyncer_FetchesTimeOut(t *testing.T) {
	t.Parallel()

	const timeout = 50 * time.Millisecond
	tests := []struct {
		name  string
		fetch func(ctx context.Context, syncer *Syncer, companyID int64) error
	}{
		{name: "sync", fetch: func(ctx context.Context, syncer *Syncer, companyID int64) error {
			_, err := syncer.SyncCompany(ctx, companyID)
			return err
		}},
		{name: "create", fetch: func(ctx context.Context, syncer *Syncer, _ int64) error {
			_, err := syncer.CreateCompany(ctx, "Globex", "ashby", "globex")
			return err
		}},
		{name: "add", fetch: func(ctx context.Context, syncer *Syncer, _ int64) error {
			_, err := syncer.AddCompany(ctx, "Globex", "ashby", "globex", "")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			syncer := New(s, map[string]PostingFetcher{"ashby": hangingFetcher{}}, Config{FetchTimeout: timeout})

			start := time.Now()
			err := tt.fetch(context.Background(), syncer, acme.ID)
			elapsed := time.Since(start)

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err = %v, want it to wrap context.DeadlineExceeded", err)
			}
			if elapsed > 2*time.Second {
				t.Errorf("gave up after %v, want about %v", elapsed, timeout)
			}
		})
	}
}

// TestSyncAll_HungBoardFailsOnlyItsOwnCompany checks a board that never
// answers costs one FetchTimeout and fails only its own company: SyncAll
// records the timeout and carries on, so the companies after it still
// sync (issue #142).
func TestSyncAll_HungBoardFailsOnlyItsOwnCompany(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	hung := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	healthy := mustCreateCompany(t, s, "Globex", "lever", "globex")
	syncer := New(s, map[string]PostingFetcher{
		"ashby": hangingFetcher{},
		"lever": &fakeFetcher{postings: map[string][]jobboard.Posting{
			"globex": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		}},
	}, Config{FetchTimeout: 50 * time.Millisecond})

	results, err := syncer.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}

	byCompany := make(map[int64]Result, len(results))
	for _, r := range results {
		byCompany[r.CompanyID] = r
	}
	if r := byCompany[hung.ID]; !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Errorf("hung company's Err = %v, want it to wrap context.DeadlineExceeded", r.Err)
	}
	if r := byCompany[healthy.ID]; r.Err != nil || r.Created != 1 {
		t.Errorf("healthy company's result = %+v, want Created=1 and no Err", r)
	}
}

// cancelAfterFetchFetcher stands in for a caller that cancels while a
// sync is past its fetch: it cancels the sync's parent context, then
// returns postings as if the board had answered just in time.
type cancelAfterFetchFetcher struct {
	cancel   context.CancelFunc
	postings []jobboard.Posting
}

func (f cancelAfterFetchFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	f.cancel()
	return f.postings, nil
}

// TestSyncCompany_CancelledAfterFetch_WritesComplete: cancellation stops
// the fetch, never the writes (#149). Once the board has answered, the
// sync is all local writes; stopping them partway would leave the
// company half-synced with a stale last_fetched_at.
func TestSyncCompany_CancelledAfterFetch_WritesComplete(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetcher := cancelAfterFetchFetcher{cancel: cancel, postings: []jobboard.Posting{
		samplePosting("job-1", "Engineer", "Engineering", "Remote"),
		samplePosting("job-2", "Designer", "Design", "Remote"),
	}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

	result, err := syncer.SyncCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("SyncCompany cancelled after its fetch: %v, want the writes to complete", err)
	}
	if result.Created != 2 {
		t.Errorf("result.Created = %d, want 2", result.Created)
	}
	company, err := s.GetCompany(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if company.LastFetchedAt.IsZero() {
		t.Error("LastFetchedAt not set, want the completed sync recorded")
	}
}

// TestSyncCompany_CancelledDuringFetch_AbortsWithoutWriting: the other
// half of #149's contract -- a caller cancelling before the board answers
// still abandons that company, and nothing is written.
func TestSyncCompany_CancelledDuringFetch_AbortsWithoutWriting(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := New(s, map[string]PostingFetcher{"ashby": hangingFetcher{}}, DefaultConfig())

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := syncer.SyncCompany(ctx, acme.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SyncCompany cancelled during its fetch: err = %v, want context.Canceled", err)
	}

	postings, err := s.ListPostingsByCompany(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	company, err := s.GetCompany(context.Background(), acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if len(postings) != 0 || !company.LastFetchedAt.IsZero() {
		t.Errorf("after a cancelled fetch: %d postings, LastFetchedAt %v, want nothing written", len(postings), company.LastFetchedAt)
	}
}
