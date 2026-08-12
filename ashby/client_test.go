package ashby

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func newTestServer(t *testing.T, fixturePath string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchPostings_ReturnsPostingsFromFixture(t *testing.T) {
	server := newTestServer(t, "testdata/job_board.json")
	client := NewClient(WithBaseURL(server.URL))

	postings, err := client.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	want := Posting{
		SourceID:        "34413f8d-26bf-4bbc-8ade-eb309a0e2245",
		Title:           "Security Engineer, Cloud",
		Department:      "Engineering",
		Team:            "Backend",
		Location:        "New York, NY (HQ)",
		EmploymentType:  "FullTime",
		WorkplaceType:   "Hybrid",
		DescriptionHTML: "<h1>About the role</h1><p>Join our security team.</p>",
		DescriptionText: "About the role\nJoin our security team.",
		JobURL:          "https://jobs.ashbyhq.com/acme/34413f8d-26bf-4bbc-8ade-eb309a0e2245",
		ApplicationURL:  "https://jobs.ashbyhq.com/acme/34413f8d-26bf-4bbc-8ade-eb309a0e2245/application",
		PublishedAt:     time.Date(2026, 4, 7, 17, 12, 35, 753000000, time.UTC),
	}

	if diff := cmp.Diff(want, postings[0], cmpopts.IgnoreFields(Posting{}, "RawPayload")); diff != "" {
		t.Fatalf("first posting mismatch (-want +got):\n%s", diff)
	}
}

func TestFetchPostings_RawPayloadMatchesOriginalJSON(t *testing.T) {
	server := newTestServer(t, "testdata/job_board.json")
	client := NewClient(WithBaseURL(server.URL))

	postings, err := client.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}

	fixtureBody, err := os.ReadFile("testdata/job_board.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var raw struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(fixtureBody, &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if len(raw.Jobs) != len(postings) {
		t.Fatalf("fixture has %d jobs, got %d postings", len(raw.Jobs), len(postings))
	}
	for i, want := range raw.Jobs {
		if diff := cmp.Diff([]byte(want), postings[i].RawPayload); diff != "" {
			t.Fatalf("posting[%d].RawPayload mismatch (-want +got):\n%s", i, diff)
		}
	}
}

func TestFetchPostings_NonOKStatus_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client := NewClient(WithBaseURL(server.URL))

	_, err := client.FetchPostings(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("FetchPostings: expected error for 404 response, got nil")
	}
}
