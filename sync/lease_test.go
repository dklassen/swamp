package sync

import (
	"context"
	"errors"
	gosync "sync"
	"testing"
	"time"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// countingFetcher returns postings and counts how often it was asked.
type countingFetcher struct {
	mu       gosync.Mutex
	calls    int
	postings []jobboard.Posting
}

func (f *countingFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.postings, nil
}

// TestSyncCompany_LeaseHeldElsewhere_SkipsWithoutFetching: while another
// sync -- in this process or another -- holds the company's lease, a
// second sync returns ErrSyncInProgress without fetching or writing
// anything (#150).
func TestSyncCompany_LeaseHeldElsewhere_SkipsWithoutFetching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if taken, err := s.AcquireSyncLease(ctx, acme.ID, "another-sync", time.Minute); err != nil || !taken {
		t.Fatalf("AcquireSyncLease = %v, %v", taken, err)
	}

	fetcher := &countingFetcher{postings: []jobboard.Posting{samplePosting("job-1", "Engineer", "Engineering", "Remote")}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	_, err := syncer.SyncCompany(ctx, acme.ID)
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("SyncCompany while the lease is held: err = %v, want ErrSyncInProgress", err)
	}
	if fetcher.calls != 0 {
		t.Errorf("board fetched %d times, want 0", fetcher.calls)
	}
	postings, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 0 {
		t.Errorf("%d postings written, want 0", len(postings))
	}
}

// gatedFetcher blocks every fetch until release is closed, after telling
// started that it has begun -- so a test can act while a sync is mid-fetch.
type gatedFetcher struct {
	started  chan struct{}
	release  chan struct{}
	postings []jobboard.Posting
}

func (f *gatedFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	f.started <- struct{}{}
	<-f.release
	return f.postings, nil
}

// TestSyncCompany_WhileAnotherSyncRuns_Skipped: a second SyncCompany
// started while the first is mid-fetch is refused with ErrSyncInProgress;
// once the first finishes, the lease is free again.
func TestSyncCompany_WhileAnotherSyncRuns_Skipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	gated := &gatedFetcher{started: make(chan struct{}, 1), release: make(chan struct{}),
		postings: []jobboard.Posting{samplePosting("job-1", "Engineer", "Engineering", "Remote")}}
	syncer := New(s, map[string]PostingFetcher{"ashby": gated}, DefaultConfig())

	first := make(chan error, 1)
	go func() {
		_, err := syncer.SyncCompany(ctx, acme.ID)
		first <- err
	}()
	<-gated.started

	if _, err := syncer.SyncCompany(ctx, acme.ID); !errors.Is(err, ErrSyncInProgress) {
		t.Errorf("second SyncCompany while the first is mid-fetch: err = %v, want ErrSyncInProgress", err)
	}
	close(gated.release)
	if err := <-first; err != nil {
		t.Fatalf("first SyncCompany: %v", err)
	}

	next := New(s, map[string]PostingFetcher{"ashby": &countingFetcher{}}, DefaultConfig())
	if _, err := next.SyncCompany(ctx, acme.ID); err != nil {
		t.Errorf("SyncCompany after the first finished: %v, want the lease free again", err)
	}
}

// TestSyncCompany_AfterFailedSync_LeaseReleased: a sync that fails still
// releases its lease, so the next sync isn't refused until it expires.
func TestSyncCompany_AfterFailedSync_LeaseReleased(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	failing := New(s, map[string]PostingFetcher{"ashby": &fakeFetcher{err: errors.New("board down")}}, DefaultConfig())
	if _, err := failing.SyncCompany(ctx, acme.ID); err == nil || errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("failing SyncCompany: err = %v, want the fetch error", err)
	}

	next := New(s, map[string]PostingFetcher{"ashby": &countingFetcher{}}, DefaultConfig())
	if _, err := next.SyncCompany(ctx, acme.ID); err != nil {
		t.Errorf("SyncCompany after a failed sync: %v, want the lease released", err)
	}
}

// staleFirstFetcher: the first fetch (run A) sees the board as it was --
// returning old -- but only once released, by which time a later fetch
// (run B) has been attempted and would see current.
type staleFirstFetcher struct {
	mu      gosync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	old     []jobboard.Posting
	current []jobboard.Posting
}

func (f *staleFirstFetcher) FetchPostings(ctx context.Context, boardSlug string) ([]jobboard.Posting, error) {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()
	if !first {
		return f.current, nil
	}
	close(f.started)
	<-f.release
	return f.old, nil
}

// TestSyncCompany_StaleFetchCantEndReopenedPostingsApplication is #150's
// motivating race. Posting X was dropped from the board once (its
// application auto-closed, and the user set it back to submitted) and is
// listed again. Run A fetched while X was still missing; run B fetches
// after X came back. Without the lease, B reopened X and then A -- acting
// on its older fetch -- closed it again and moved the application to
// posting_closed, which no later sync undoes. With it, B is skipped while
// A holds the company, and the application is never touched.
func TestSyncCompany_StaleFetchCantEndReopenedPostingsApplication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	x := samplePosting("job-x", "Engineer", "Engineering", "Remote")

	setup := &fakeFetcher{postings: map[string][]jobboard.Posting{"acme": {x}}}
	seed := New(s, map[string]PostingFetcher{"ashby": setup}, DefaultConfig())
	if _, err := seed.SyncCompany(ctx, acme.ID); err != nil {
		t.Fatalf("initial SyncCompany: %v", err)
	}
	postings, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	xID := postings[0].ID
	if _, err := s.CreateApplication(ctx, xID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	setup.postings["acme"] = nil
	if _, err := seed.SyncCompany(ctx, acme.ID); err != nil { // X dropped: closed
		t.Fatalf("closing SyncCompany: %v", err)
	}
	if _, err := s.UpdateApplicationStatus(ctx, xID, store.ApplicationStatusSubmitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	fetcher := &staleFirstFetcher{started: make(chan struct{}), release: make(chan struct{}), old: nil, current: []jobboard.Posting{x}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	runA := make(chan error, 1)
	go func() {
		_, err := syncer.SyncCompany(ctx, acme.ID)
		runA <- err
	}()
	<-fetcher.started
	_, errB := syncer.SyncCompany(ctx, acme.ID)
	close(fetcher.release)
	if err := <-runA; err != nil {
		t.Fatalf("run A: %v", err)
	}
	if !errors.Is(errB, ErrSyncInProgress) {
		t.Errorf("run B while A holds the company: err = %v, want ErrSyncInProgress", errB)
	}

	setup.postings["acme"] = []jobboard.Posting{x}
	if _, err := seed.SyncCompany(ctx, acme.ID); err != nil {
		t.Fatalf("next SyncCompany: %v", err)
	}
	posting, err := s.GetPosting(ctx, xID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	application, err := s.GetApplication(ctx, xID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if posting.ListingStatus != "open" || application.Status != store.ApplicationStatusSubmitted {
		t.Errorf("after the next sync: posting %s, application %s; want open and %s", posting.ListingStatus, application.Status, store.ApplicationStatusSubmitted)
	}
}

// TestSyncer_ReleaseHeldLeases_FreesInFlightSyncsLeases: a process that
// exits with a sync still in flight -- the TUI quitting mid-refresh or
// mid-sync-all abandons its running command, then closes the database --
// releases the leases it holds on the way out, so the next sync of that
// company isn't refused until the lease expires (#153).
func TestSyncer_ReleaseHeldLeases_FreesInFlightSyncsLeases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	gated := &gatedFetcher{started: make(chan struct{}, 1), release: make(chan struct{})}
	exiting := New(s, map[string]PostingFetcher{"ashby": gated}, DefaultConfig())

	inFlight := make(chan error, 1)
	go func() {
		_, err := exiting.SyncCompany(ctx, acme.ID)
		inFlight <- err
	}()
	<-gated.started
	if err := exiting.ReleaseHeldLeases(ctx); err != nil {
		t.Fatalf("ReleaseHeldLeases: %v", err)
	}

	next := New(s, map[string]PostingFetcher{"ashby": &countingFetcher{}}, DefaultConfig())
	if _, err := next.SyncCompany(ctx, acme.ID); err != nil {
		t.Errorf("SyncCompany after the exiting process released its leases: %v, want the lease free", err)
	}
	close(gated.release)
	<-inFlight
}
