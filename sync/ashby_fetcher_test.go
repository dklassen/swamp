package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dklassen/swamp/ashby"
)

func TestAshbyFetcher_TranslatesAshbyPostingIntoSyncPosting(t *testing.T) {
	body, err := os.ReadFile("../ashby/testdata/job_board.json")
	if err != nil {
		t.Fatalf("read ashby fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	fetcher := NewAshbyFetcher(ashby.NewClient(ashby.WithBaseURL(server.URL)))
	postings, err := fetcher.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	p := postings[0]
	if p.Title != "Security Engineer, Cloud" || p.Department != "Engineering" || p.Team != "Backend" {
		t.Errorf("translated posting = %+v, missing fields ashby.Posting has and sync.Posting should carry over", p)
	}
	if len(p.RawPayload) == 0 {
		t.Error("RawPayload is empty, want the original job JSON preserved")
	}
}
