package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dklassen/swamp/greenhouse"
)

func TestGreenhouseFetcher_TranslatesGreenhousePostingIntoSyncPosting(t *testing.T) {
	body, err := os.ReadFile("../greenhouse/testdata/job_board.json")
	if err != nil {
		t.Fatalf("read greenhouse fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	fetcher := NewGreenhouseFetcher(greenhouse.NewClient(greenhouse.WithBaseURL(server.URL)))
	postings, err := fetcher.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	p := postings[0]
	if p.Title != "Security Engineer, Cloud" || p.Department != "Engineering" {
		t.Errorf("translated posting = %+v, missing fields greenhouse.Posting has and sync.Posting should carry over", p)
	}
	if p.EmploymentType != "" || p.WorkplaceType != "" || p.Team != "" {
		t.Errorf("translated posting = %+v, want EmploymentType/WorkplaceType/Team left empty -- Greenhouse doesn't provide them", p)
	}
	if len(p.RawPayload) == 0 {
		t.Error("RawPayload is empty, want the original job JSON preserved")
	}
}
