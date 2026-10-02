package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/store"
)

// searchStage is a Stage over two companies' postings: three open at
// Acme (one interested with no application, one started, one submitted),
// one closed and one archived at Acme, and one open at Beta.
type searchStage struct {
	st                            *Stage
	acme, beta                    store.Company
	shortlist, started, submitted store.Posting
	closed, archived, betaPosting store.Posting
	startedApp                    store.Application
}

func newSearchStage(t *testing.T) searchStage {
	t.Helper()
	st, s, _ := newTestStage(t)
	ctx := context.Background()
	f := searchStage{st: st, acme: mustCreateCompany(t, s, "Acme"), beta: mustCreateCompany(t, s, "Beta")}
	f.shortlist = mustUpsertPosting(t, s, f.acme.ID, "1", "Platform Engineer")
	f.started = mustUpsertPosting(t, s, f.acme.ID, "2", "Data Engineer")
	f.submitted = mustUpsertPosting(t, s, f.acme.ID, "3", "Designer")
	f.closed = mustUpsertPosting(t, s, f.acme.ID, "4", "Writer")
	f.archived = mustUpsertPosting(t, s, f.acme.ID, "5", "Shelved Role")
	f.betaPosting = mustUpsertPosting(t, s, f.beta.ID, "6", "Engineer")

	mustMarkInterested(t, s, f.shortlist.ID)
	var err error
	if f.startedApp, err = s.CreateApplication(ctx, f.started.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := s.CreateApplication(ctx, f.submitted.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := s.UpdateApplicationStatus(ctx, f.submitted.ID, store.ApplicationStatusSubmitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	if err := s.MarkPostingClosed(ctx, f.closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if _, err := s.SetPostingArchived(ctx, f.archived.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	return f
}

func matchIDs(matches []SearchMatch) []int64 {
	ids := make([]int64, len(matches))
	for i, m := range matches {
		ids[i] = m.Posting.ID
	}
	return ids
}

func postingIDs(postings ...store.Posting) []int64 {
	ids := make([]int64, len(postings))
	for i, p := range postings {
		ids[i] = p.ID
	}
	return ids
}

func TestSearch_FiltersAndDefaults(t *testing.T) {
	t.Parallel()
	f := newSearchStage(t)
	yes, no := true, false

	tests := []struct {
		name string
		opts SearchOptions
		want []int64
	}{
		{"defaults: open, not archived, ID order", SearchOptions{},
			postingIDs(f.shortlist, f.started, f.submitted, f.betaPosting)},
		{"company", SearchOptions{CompanyID: f.beta.ID}, postingIDs(f.betaPosting)},
		{"with an application", SearchOptions{HasApplication: &yes}, postingIDs(f.started, f.submitted)},
		{"application statuses", SearchOptions{ApplicationStatuses: []store.ApplicationStatus{store.ApplicationStatusSubmitted}},
			postingIDs(f.submitted)},
		{"interested shortlist: interested, no application", SearchOptions{Interested: &yes, HasApplication: &no},
			postingIDs(f.shortlist)},
		{"closed", SearchOptions{ListingStatus: "closed"}, postingIDs(f.closed)},
		{"any listing status", SearchOptions{ListingStatus: "any"},
			postingIDs(f.shortlist, f.started, f.submitted, f.closed, f.betaPosting)},
		{"archived included", SearchOptions{IncludeArchived: true},
			postingIDs(f.shortlist, f.started, f.submitted, f.archived, f.betaPosting)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := f.st.Search(context.Background(), tt.opts)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if diff := cmp.Diff(tt.want, matchIDs(got.Postings)); diff != "" {
				t.Errorf("IDs mismatch (-want +got):\n%s", diff)
			}
			if got.Total != len(tt.want) || got.NextCursor != nil {
				t.Errorf("Total %d, NextCursor %v; want %d, nil", got.Total, got.NextCursor, len(tt.want))
			}
		})
	}
}

// TestSearch_MatchCarriesItsApplication: a match says which company it's
// at and, when it has one, its application -- what the agent needs to
// go on to stage_prepare and the document tools.
func TestSearch_MatchCarriesItsApplication(t *testing.T) {
	t.Parallel()
	f := newSearchStage(t)
	yes := true

	got, err := f.st.Search(context.Background(), SearchOptions{CompanyID: f.acme.ID, HasApplication: &yes, Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Postings) != 1 {
		t.Fatalf("got %d matches, want 1", len(got.Postings))
	}
	m := got.Postings[0]
	if m.Posting.ID != f.started.ID || m.Posting.Title != "Data Engineer" || m.CompanyName != "Acme" || m.ListingStatus != "open" {
		t.Errorf("match = %+v, want the started Data Engineer posting at Acme, open", m)
	}
	if m.ApplicationID == nil || *m.ApplicationID != f.startedApp.ID ||
		m.ApplicationStatus == nil || *m.ApplicationStatus != store.ApplicationStatusStarted {
		t.Errorf("match application = %v %v, want %d started", m.ApplicationID, m.ApplicationStatus, f.startedApp.ID)
	}
}

// TestSearch_PagesWithOpaqueCursors: following NextCursor walks the whole
// result a page at a time, with the same Total on every page, and ends
// with a nil NextCursor (#221).
func TestSearch_PagesWithOpaqueCursors(t *testing.T) {
	t.Parallel()
	f := newSearchStage(t)
	ctx := context.Background()

	all, err := f.st.Search(ctx, SearchOptions{ListingStatus: "any", IncludeArchived: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var paged []int64
	opts := SearchOptions{ListingStatus: "any", IncludeArchived: true, Limit: 2}
	for pages := 1; ; pages++ {
		if pages > 10 {
			t.Fatal("paging didn't end")
		}
		page, err := f.st.Search(ctx, opts)
		if err != nil {
			t.Fatalf("Search page %d: %v", pages, err)
		}
		if page.Total != len(all.Postings) {
			t.Errorf("page %d Total = %d, want %d", pages, page.Total, len(all.Postings))
		}
		paged = append(paged, matchIDs(page.Postings)...)
		if page.NextCursor == nil {
			if pages != 3 {
				t.Errorf("%d pages, want 3 of at most 2 for %d postings", pages, len(all.Postings))
			}
			break
		}
		opts.Cursor = *page.NextCursor
	}
	if diff := cmp.Diff(matchIDs(all.Postings), paged); diff != "" {
		t.Errorf("pages don't concatenate to the whole result (-want +got):\n%s", diff)
	}
}

// TestSearch_CursorFromAnotherSearch: a cursor passed with other filters
// is an error, not a page of a search it wasn't made for. Page size may
// change between pages, and application statuses given in another order are
// the same filters.
func TestSearch_CursorFromAnotherSearch(t *testing.T) {
	t.Parallel()
	f := newSearchStage(t)
	ctx := context.Background()
	statuses := []store.ApplicationStatus{store.ApplicationStatusStarted, store.ApplicationStatusSubmitted}

	first, err := f.st.Search(ctx, SearchOptions{ApplicationStatuses: statuses, Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if first.NextCursor == nil {
		t.Fatal("first page has no NextCursor")
	}
	next := *first.NextCursor

	if _, err := f.st.Search(ctx, SearchOptions{CompanyID: f.beta.ID, ApplicationStatuses: statuses, Cursor: next}); !errors.Is(err, cursor.ErrMismatch) {
		t.Errorf("cursor with another company: err = %v, want cursor.ErrMismatch", err)
	}
	reordered := []store.ApplicationStatus{store.ApplicationStatusSubmitted, store.ApplicationStatusStarted}
	second, err := f.st.Search(ctx, SearchOptions{ApplicationStatuses: reordered, Limit: 5, Cursor: next})
	if err != nil {
		t.Fatalf("same filters, statuses reordered, bigger page: %v", err)
	}
	if diff := cmp.Diff(postingIDs(f.submitted), matchIDs(second.Postings)); diff != "" {
		t.Errorf("second page mismatch (-want +got):\n%s", diff)
	}
}

func TestSearch_InvalidOptions(t *testing.T) {
	t.Parallel()
	f := newSearchStage(t)

	for name, opts := range map[string]SearchOptions{
		"unknown listing status": {ListingStatus: "pending"},
		"unknown sort":           {Sort: "title"},
		"garbled cursor":         {Cursor: "not a cursor!"},
	} {
		if _, err := f.st.Search(context.Background(), opts); err == nil {
			t.Errorf("%s: Search(%+v) = nil error, want one", name, opts)
		}
	}
}

// TestSearch_LimitDefaultsAndCap: no Limit is 50; more than 100 is 100.
func TestSearch_LimitDefaultsAndCap(t *testing.T) {
	t.Parallel()
	st, s, _ := newTestStage(t)
	acme := mustCreateCompany(t, s, "Acme")
	for i := range 120 {
		mustUpsertPosting(t, s, acme.ID, string(rune('a'+i/26))+string(rune('a'+i%26)), "Engineer")
	}

	for _, tt := range []struct{ limit, want int }{{0, 50}, {500, 100}, {7, 7}} {
		got, err := st.Search(context.Background(), SearchOptions{Limit: tt.limit})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got.Postings) != tt.want || got.Total != 120 {
			t.Errorf("Limit %d: %d matches, Total %d; want %d, 120", tt.limit, len(got.Postings), got.Total, tt.want)
		}
	}
}
