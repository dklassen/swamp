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
