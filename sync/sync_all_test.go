package sync

import (
	"context"
	"errors"
	"testing"
)

func TestSyncAll_OneCompanyFailsFetch_OthersStillProcessed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	good := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	_ = mustCreateCompany(t, s, "Globex", "ashby", "globex")

	fetcher := &perBoardFetcher{
		postings: map[string][]Posting{
			"acme": {samplePosting("job-1", "Engineer", "Engineering", "Remote")},
		},
		errBoards: map[string]error{
			"globex": errors.New("connection refused"),
		},
	}

	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})
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
