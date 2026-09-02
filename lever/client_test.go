package lever

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

	"github.com/dklassen/swamp/jobboard"
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
	server := newTestServer(t, "testdata/postings.json")
	client := NewClient(WithBaseURL(server.URL))

	postings, err := client.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}

	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}

	want := jobboard.Posting{
		SourceID:        "34413f8d-26bf-4bbc-8ade-eb309a0e2245",
		Title:           "Security Engineer, Cloud",
		Department:      "Engineering",
		Team:            "Backend",
		Location:        "New York, NY",
		EmploymentType:  "Full-time",
		WorkplaceType:   "hybrid",
		DescriptionHTML: "<h1>About the role</h1><p>Join our security team.</p>",
		DescriptionText: "About the role\nJoin our security team.",
		JobURL:          "https://jobs.lever.co/acme/34413f8d-26bf-4bbc-8ade-eb309a0e2245",
		ApplicationURL:  "https://jobs.lever.co/acme/34413f8d-26bf-4bbc-8ade-eb309a0e2245/apply",
		PublishedAt:     time.UnixMilli(1749313955753).UTC(),
	}

	if diff := cmp.Diff(want, postings[0], cmpopts.IgnoreFields(jobboard.Posting{}, "RawPayload")); diff != "" {
		t.Fatalf("first posting mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchPostings_MissingDepartment_LeavesItEmpty covers a real
// behavior confirmed against Lever's live API (Ro's board, verified
// 2026-09-01): categories.department is omitted entirely from the JSON
// when a posting has no department set, not sent as an empty string --
// the second fixture posting mirrors that.
func TestFetchPostings_MissingDepartment_LeavesItEmpty(t *testing.T) {
	server := newTestServer(t, "testdata/postings.json")
	client := NewClient(WithBaseURL(server.URL))

	postings, err := client.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}
	if postings[1].Department != "" {
		t.Fatalf("postings[1].Department = %q, want empty (department omitted from source JSON)", postings[1].Department)
	}
}

func TestFetchPostings_RawPayloadMatchesOriginalJSON(t *testing.T) {
	server := newTestServer(t, "testdata/postings.json")
	client := NewClient(WithBaseURL(server.URL))

	postings, err := client.FetchPostings(context.Background(), "acme")
	if err != nil {
		t.Fatalf("FetchPostings: %v", err)
	}

	fixtureBody, err := os.ReadFile("testdata/postings.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(fixtureBody, &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if len(raw) != len(postings) {
		t.Fatalf("fixture has %d postings, got %d postings", len(raw), len(postings))
	}
	for i, want := range raw {
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
