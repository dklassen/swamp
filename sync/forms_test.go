package sync

import (
	"context"
	"errors"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

var boardForm = jobboard.ApplicationForm{
	Documents: map[document.Type]jobboard.Requirement{document.Resume: jobboard.Required},
}

// formBoard returns boardForm for every job but those in fail, recording
// each job fetched. during, if set, runs inside each fetch.
type formBoard struct {
	mu      gosync.Mutex
	fail    map[string]bool
	fetched []string
	during  func()
}

func (b *formBoard) FetchApplicationForm(_ context.Context, _, jobID string) (jobboard.ApplicationForm, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.during != nil {
		b.during()
	}
	b.fetched = append(b.fetched, jobID)
	if b.fail[jobID] {
		return jobboard.ApplicationForm{}, errors.New("job " + jobID + " has no form")
	}
	return boardForm, nil
}

// formSyncer syncs Ashby companies from postings, fetching their forms
// from board.
func formSyncer(s testutil.TestApp, postings *fakeFetcher, board *formBoard) *Syncer {
	forms := service.NewFormService(s.Store, map[string]service.FormFetcher{jobboard.Ashby: board}, service.FormConfig{FetchTimeout: time.Second})
	return New(s.Companies, s.Postings, forms, map[string]PostingFetcher{jobboard.Ashby: postings}, DefaultConfig())
}

func TestSyncCompany_NewPosting_FormFetched(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	postings := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	board := &formBoard{}

	result, err := formSyncer(s, postings, board).SyncCompany(t.Context(), acme.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.FormsFetched != 1 || result.FormsFailed != 0 {
		t.Errorf("FormsFetched, FormsFailed = %d, %d, want 1, 0", result.FormsFetched, result.FormsFailed)
	}
	listed, err := s.Postings.ListByCompany(t.Context(), acme.ID)
	if err != nil {
		t.Fatalf("ListByCompany: %v", err)
	}
	stored, err := s.Forms.Get(t.Context(), listed[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(&boardForm, stored); diff != "" {
		t.Errorf("stored form (-want +got):\n%s", diff)
	}
}

func TestSyncCompany_FailedFormFetch_PostingsStillSynced(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	postings := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {
			samplePosting("job-1", "Engineer", "Engineering", "Remote"),
			samplePosting("job-2", "Designer", "Design", "Remote"),
		},
	}}
	board := &formBoard{fail: map[string]bool{"job-2": true}}

	result, err := formSyncer(s, postings, board).SyncCompany(t.Context(), acme.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Created = %d, want 2", result.Created)
	}
	if result.FormsFetched != 1 || result.FormsFailed != 1 {
		t.Errorf("FormsFetched, FormsFailed = %d, %d, want 1, 1", result.FormsFetched, result.FormsFailed)
	}
}

func TestSyncCompany_Unfinished_FetchesNoForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arrange func(*testing.T, testutil.TestApp, int64, *fakeFetcher)
		want    error
	}{
		{"another sync holds the lease", func(t *testing.T, s testutil.TestApp, companyID int64, _ *fakeFetcher) {
			t.Helper()
			if _, err := s.Companies.AcquireSyncLease(t.Context(), companyID, "other", time.Minute); err != nil {
				t.Fatalf("AcquireSyncLease: %v", err)
			}
		}, ErrSyncInProgress},
		{"the board fails", func(_ *testing.T, _ testutil.TestApp, _ int64, postings *fakeFetcher) {
			postings.err = errBoardDown
		}, errBoardDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
			postings := &fakeFetcher{}
			tt.arrange(t, s, acme.ID, postings)
			board := &formBoard{}

			_, err := formSyncer(s, postings, board).SyncCompany(t.Context(), acme.ID)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SyncCompany: err = %v, want %v", err, tt.want)
			}
			if len(board.fetched) != 0 {
				t.Errorf("fetched %v, want no form fetch", board.fetched)
			}
		})
	}
}

var errBoardDown = errors.New("board unavailable")

func TestSyncCompany_FormsFetchedOutsideTheLease(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	postings := &fakeFetcher{postings: map[string][]jobboard.Posting{
		"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
	}}
	board := &formBoard{}
	board.during = func() {
		taken, err := s.Companies.AcquireSyncLease(context.Background(), acme.ID, "probe", time.Minute)
		if err != nil {
			t.Errorf("AcquireSyncLease: %v", err)
			return
		}
		if !taken {
			t.Error("the company's lease was held during the form fetch")
			return
		}
		if err := s.Companies.ReleaseSyncLease(context.Background(), acme.ID, "probe"); err != nil {
			t.Errorf("ReleaseSyncLease: %v", err)
		}
	}

	if _, err := formSyncer(s, postings, board).SyncCompany(t.Context(), acme.ID); err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if len(board.fetched) != 1 {
		t.Errorf("fetched %v, want job-1's form", board.fetched)
	}
}
