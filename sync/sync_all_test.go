package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/jobboard"
)

func TestSyncAll_OneCompanyFailsFetch_OthersStillProcessed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	good := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	_ = mustCreateCompany(t, s, "Globex", "ashby", "globex")

	fetcher := &perBoardFetcher{
		postings: map[string][]jobboard.Posting{
			"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		},
		errBoards: map[string]error{
			"globex": errors.New("connection refused"),
		},
	}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())
	results, err := syncer.SyncAll(ctx)
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	var goodResult, badResult *Result
	for i := range results {
		switch results[i].CompanyID {
		case good.ID:
			goodResult = &results[i]
		default:
			badResult = &results[i]
		}
	}

	if goodResult == nil || goodResult.Err != nil || goodResult.Created != 1 {
		t.Fatalf("goodResult = %+v, want Created=1 Err=nil", goodResult)
	}
	if badResult == nil || badResult.Err == nil {
		t.Fatalf("badResult = %+v, want a non-nil Err", badResult)
	}
}

// TestSyncResults_CarryCompanyName: every result names its company,
// including one whose fetch failed, so callers reporting results don't
// have to carry the name alongside (#151).
func TestSyncResults_CarryCompanyName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	fetcher := &perBoardFetcher{
		postings:  map[string][]jobboard.Posting{"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")}},
		errBoards: map[string]error{"globex": errors.New("connection refused")},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher}, DefaultConfig())

	one, err := syncer.SyncCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("SyncCompany: %v", err)
	}
	if one.Name != "Acme" {
		t.Errorf("SyncCompany result Name = %q, want Acme", one.Name)
	}

	all, err := syncer.SyncAll(ctx)
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	var names []string
	for _, r := range all {
		names = append(names, r.Name)
	}
	if diff := cmp.Diff([]string{"Acme", "Globex"}, names); diff != "" {
		t.Errorf("SyncAll result names mismatch (-want +got):\n%s", diff)
	}
}
