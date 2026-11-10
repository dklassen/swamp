package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

// searchFixture is a server over two companies' postings: three open at
// Acme (one interested with no application, one started, one submitted),
// one closed and one archived at Acme, and one open at Beta.
type searchFixture struct {
	cs                            *mcp.ClientSession
	acme, beta                    company.Company
	shortlist, started, submitted posting.Posting
	closed, archived, betaPosting posting.Posting
	startedApp                    application.Application
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()
	srv, s, _ := newTestServer(t)
	ctx := context.Background()
	f := searchFixture{acme: mustCreateCompany(t, s, "Acme"), beta: mustCreateCompany(t, s, "Beta")}
	f.shortlist = mustUpsertPosting(t, s, f.acme.ID, "1", "Platform Engineer")
	f.started = mustUpsertPosting(t, s, f.acme.ID, "2", "Data Engineer")
	f.submitted = mustUpsertPosting(t, s, f.acme.ID, "3", "Designer")
	f.closed = mustUpsertPosting(t, s, f.acme.ID, "4", "Writer")
	f.archived = mustUpsertPosting(t, s, f.acme.ID, "5", "Shelved Role")
	f.betaPosting = mustUpsertPosting(t, s, f.beta.ID, "6", "Engineer")

	mustMarkInterested(t, s, f.shortlist.ID)
	started, err := createApplication(ctx, s, f.started.ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	f.startedApp = *started
	submitted, err := createApplication(ctx, s, f.submitted.ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	if _, err := s.Applications.SetStatus(ctx, submitted.ID, application.Submitted); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if err := s.Postings.Close(ctx, f.closed.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Postings.SetArchived(ctx, f.archived.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	f.cs = connectClient(t, srv)
	return f
}

func postingIDs(postings ...posting.Posting) []int64 {
	ids := make([]int64, len(postings))
	for i, p := range postings {
		ids[i] = p.ID
	}
	return ids
}

func TestSearchPostings_FiltersAndDefaults(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	tests := []struct {
		name string
		args map[string]any
		want []int64
	}{
		{"defaults: open, not archived, ID order", map[string]any{}, postingIDs(f.shortlist, f.started, f.submitted, f.betaPosting)},
		{"company", map[string]any{"CompanyID": f.beta.ID}, postingIDs(f.betaPosting)},
		{"with an application", map[string]any{"HasApplication": true}, postingIDs(f.started, f.submitted)},
		{"application statuses", map[string]any{"ApplicationStatuses": []string{application.Submitted.String()}}, postingIDs(f.submitted)},
		{"interested shortlist: interested, no application", map[string]any{"Interested": true, "HasApplication": false}, postingIDs(f.shortlist)},
		{"closed", map[string]any{"ListingStatus": posting.ListingClosed.String()}, postingIDs(f.closed)},
		{"any listing status", map[string]any{"ListingStatus": anyListingStatus}, postingIDs(f.shortlist, f.started, f.submitted, f.closed, f.betaPosting)},
		{"archived included", map[string]any{"IncludeArchived": true}, postingIDs(f.shortlist, f.started, f.submitted, f.archived, f.betaPosting)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := callTool[searchPostingsResult](t, f.cs, "search_postings", tt.args)
			if diff := cmp.Diff(tt.want, searchIDs(got)); diff != "" {
				t.Errorf("IDs mismatch (-want +got):\n%s", diff)
			}
			if got.Total != len(tt.want) || got.NextCursor != nil {
				t.Errorf("Total %d, NextCursor %v; want %d, nil", got.Total, got.NextCursor, len(tt.want))
			}
		})
	}
}

// TestSearchPostings_MatchCarriesItsApplication: a match says which company
// it's at and, when it has one, its application.
func TestSearchPostings_MatchCarriesItsApplication(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	got := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"CompanyID": f.acme.ID, "HasApplication": true, "Limit": 1})
	if len(got.Postings) != 1 {
		t.Fatalf("got %d matches, want 1", len(got.Postings))
	}
	m := got.Postings[0]
	if m.Posting.ID != f.started.ID || m.Posting.Title != "Data Engineer" || m.CompanyName != "Acme" || m.ListingStatus != posting.ListingOpen.String() {
		t.Errorf("match = %+v, want the started Data Engineer posting at Acme, open", m)
	}
	if m.Posting.FirstSeenAt.IsZero() || !m.Posting.FirstSeenAt.Equal(f.started.FirstSeenAt) {
		t.Errorf("match FirstSeenAt = %v, want the posting's %v", m.Posting.FirstSeenAt, f.started.FirstSeenAt)
	}
	if m.ApplicationID == nil || *m.ApplicationID != f.startedApp.ID ||
		m.ApplicationStatus == nil || *m.ApplicationStatus != application.Started.String() {
		t.Errorf("match application = %v %v, want %d started", m.ApplicationID, m.ApplicationStatus, f.startedApp.ID)
	}
}

// TestSearchPostings_PagesInEveryOrder: following NextCursor walks the
// whole result a page at a time, with the same Total on every page, and
// ends with a nil NextCursor.
func TestSearchPostings_PagesInEveryOrder(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)
	for _, order := range sortOrders {
		t.Run(order.Name(), func(t *testing.T) {
			t.Parallel()
			args := map[string]any{"Sort": order.Name(), "ListingStatus": anyListingStatus, "IncludeArchived": true}
			all := callTool[searchPostingsResult](t, f.cs, "search_postings", args)
			args["Limit"] = 2
			var paged []int64
			for pages := 1; ; pages++ {
				if pages > 10 {
					t.Fatal("paging didn't end")
				}
				page := callTool[searchPostingsResult](t, f.cs, "search_postings", args)
				if page.Total != len(all.Postings) {
					t.Errorf("page %d Total = %d, want %d", pages, page.Total, len(all.Postings))
				}
				paged = append(paged, searchIDs(page)...)
				if page.NextCursor == nil {
					if pages != 3 {
						t.Errorf("%d pages, want 3 of at most 2 for %d postings", pages, len(all.Postings))
					}
					break
				}
				args["Cursor"] = *page.NextCursor
			}
			if diff := cmp.Diff(searchIDs(all), paged); diff != "" {
				t.Errorf("pages don't concatenate to the whole result (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSearchPostings_CursorIsBoundToTheFilters: a cursor passed with other
// filters is refused, not a page of a search it wasn't made for. Page size
// may change between pages, and application statuses given in another
// order are the same filters.
func TestSearchPostings_CursorIsBoundToTheFilters(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)
	statuses := []string{application.Started.String(), application.Submitted.String()}

	first := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"ApplicationStatuses": statuses, "Limit": 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no NextCursor")
	}
	next := *first.NextCursor

	if msg := callToolError(t, f.cs, "search_postings", map[string]any{"CompanyID": f.beta.ID, "ApplicationStatuses": statuses, "Cursor": next}); !strings.Contains(msg, "belongs to a different search") {
		t.Errorf("cursor with another company: error %q, want it to belong to a different search", msg)
	}
	reordered := []string{application.Submitted.String(), application.Started.String()}
	second := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"ApplicationStatuses": reordered, "Limit": 5, "Cursor": next})
	if diff := cmp.Diff(postingIDs(f.submitted), searchIDs(second)); diff != "" {
		t.Errorf("second page mismatch (-want +got):\n%s", diff)
	}
}

func TestSearchPostings_InvalidOptions(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	for name, args := range map[string]map[string]any{
		"unknown listing status": {"ListingStatus": "pending"},
		"unknown sort":           {"Sort": "title"},
		"garbled cursor":         {"Cursor": "not a cursor!"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			callToolError(t, f.cs, "search_postings", args)
		})
	}
}

// TestSearchPostings_LimitDefaultsAndCap: no Limit is 50; more than 100 is
// 100.
func TestSearchPostings_LimitDefaultsAndCap(t *testing.T) {
	t.Parallel()
	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	for i := range 120 {
		mustUpsertPosting(t, s, acme.ID, string(rune('a'+i/26))+string(rune('a'+i%26)), "Engineer")
	}
	cs := connectClient(t, srv)

	for _, tt := range []struct{ limit, want int }{{0, 50}, {500, 100}, {7, 7}} {
		got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Limit": tt.limit})
		if len(got.Postings) != tt.want || got.Total != 120 {
			t.Errorf("Limit %d: %d matches, Total %d; want %d, 120", tt.limit, len(got.Postings), got.Total, tt.want)
		}
	}
}

// TestSortOrders_EveryStoreOrderIsRegistered: a store order can't reach
// the agent without a name and a note on how stable it is to page.
func TestSortOrders_EveryStoreOrderIsRegistered(t *testing.T) {
	t.Parallel()
	registered := map[posting.Order]bool{}
	for _, order := range sortOrders {
		if order.Description == "" {
			t.Errorf("%s has no description", order.Name())
		}
		registered[order.Order] = true
	}
	for _, order := range posting.Orders() {
		if !registered[order] {
			t.Errorf("store order %s isn't in SortOrders", order)
		}
	}
	if sortOrders[0].Name() != posting.OrderIDAsc.String() {
		t.Errorf("first (default) order = %s, want %s", sortOrders[0].Name(), posting.OrderIDAsc.String())
	}
}

// pageAll follows NextCursor from args to the last page and returns every
// match.
func pageAll(t *testing.T, cs *mcp.ClientSession, args map[string]any) []int64 {
	t.Helper()
	var ids []int64
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging didn't end")
		}
		page := callTool[searchPostingsResult](t, cs, "search_postings", args)
		ids = append(ids, searchIDs(page)...)
		if page.NextCursor == nil {
			return ids
		}
		args["Cursor"] = *page.NextCursor
	}
}

// TestSearchPostings_IDDescIsNewestToSwampFirst, and a cursor from one
// order can't page another.
func TestSearchPostings_IDDescIsNewestToSwampFirst(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	got := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"Sort": posting.OrderIDDesc.String(), "Limit": 2})
	if diff := cmp.Diff(postingIDs(f.betaPosting, f.submitted), searchIDs(got)); diff != "" {
		t.Errorf("first page mismatch (-want +got):\n%s", diff)
	}
	if got.NextCursor == nil {
		t.Fatal("no NextCursor")
	}
	if msg := callToolError(t, f.cs, "search_postings", map[string]any{"Cursor": *got.NextCursor}); !strings.Contains(msg, "belongs to a different search") {
		t.Errorf("id_desc cursor used with id_asc: error %q, want it to belong to a different search", msg)
	}
}

// TestSearchPostings_PublishedDescPagesNewestOnTheBoardFirst: dated
// postings newest first, undated last; each match shows its publish date.
func TestSearchPostings_PublishedDescPagesNewestOnTheBoardFirst(t *testing.T) {
	t.Parallel()
	srv, s, _ := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	ingest := func(sourceID string, published time.Time) posting.Posting {
		t.Helper()
		ingested, err := s.Postings.Ingest(ctx, acme.ID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: "Role " + sourceID, PublishedAt: posting.OptionalTime{Time: published}, RawPayload: "{}"})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		return ingested.Posting
	}
	day := func(d int) time.Time { return time.Date(2026, 9, d, 12, 0, 0, 0, time.UTC) }
	older := ingest("1", day(1))
	undated := ingest("2", time.Time{})
	newest := ingest("3", day(20))
	middle := ingest("4", day(10))
	cs := connectClient(t, srv)
	sort := posting.OrderPublishedDesc.String()

	got := pageAll(t, cs, map[string]any{"Sort": sort, "Limit": 1})
	if diff := cmp.Diff(postingIDs(newest, middle, older, undated), got); diff != "" {
		t.Errorf("order mismatch (-want +got):\n%s", diff)
	}

	first := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Sort": sort, "Limit": 4})
	if newestAt, undatedAt := first.Postings[0].Posting.PublishedAt, first.Postings[3].Posting.PublishedAt; newestAt == nil || !newestAt.Equal(day(20)) || undatedAt != nil {
		t.Errorf("PublishedAt = %v ... %v, want %v ... null", newestAt, undatedAt, day(20))
	}
}

// A cursor belongs to its query: another query's is refused, the same
// query spaced or cased differently finds the same postings and isn't.
func TestSearchPostings_CursorIsBoundToTheQuery(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	first := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"Query": "engineer", "Limit": 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no NextCursor")
	}
	next := *first.NextCursor

	if msg := callToolError(t, f.cs, "search_postings", map[string]any{"Query": "designer", "Cursor": next}); !strings.Contains(msg, "belongs to a different search") {
		t.Errorf("cursor with another query: error %q, want it to belong to a different search", msg)
	}
	second := callTool[searchPostingsResult](t, f.cs, "search_postings", map[string]any{"Query": "  ENGINEER ", "Limit": 5, "Cursor": next})
	if diff := cmp.Diff(postingIDs(f.started, f.betaPosting), searchIDs(second)); diff != "" {
		t.Errorf("second page (-want +got):\n%s", diff)
	}
}

func TestSearchPostings_ScoreDescPagesBestFitFirst(t *testing.T) {
	t.Parallel()
	srv, s, _ := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	low := mustUpsertPosting(t, s, acme.ID, "1", "Low")
	high := mustUpsertPosting(t, s, acme.ID, "2", "High")
	unscored := mustUpsertPosting(t, s, acme.ID, "3", "Unscored")
	if _, err := recordScores(ctx, s, score.Provenance{Model: "m", ProfileSHA256: testutil.MustWriteProfile(t, s, "p")}, []score.Entry{
		{PostingID: low.ID, Score: 2, Reason: "weak", ContentSHA256: low.ContentSHA256},
		{PostingID: high.ID, Score: 5, Reason: "strong", ContentSHA256: high.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
	cs := connectClient(t, srv)

	got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Sort": posting.OrderScoreDesc.String()})
	if diff := cmp.Diff([]int64{high.ID, low.ID, unscored.ID}, searchIDs(got)); diff != "" {
		t.Errorf("order (-want +got):\n%s", diff)
	}
	scores := make([]int, len(got.Postings))
	for i, m := range got.Postings {
		scores[i] = m.Score
	}
	if diff := cmp.Diff([]int{5, 2, 0}, scores); diff != "" {
		t.Errorf("scores (-want +got):\n%s", diff)
	}
	if paged := pageAll(t, cs, map[string]any{"Sort": posting.OrderScoreDesc.String(), "Limit": 1}); !cmp.Equal(searchIDs(got), paged) {
		t.Errorf("paged one at a time = %v, want %v", paged, searchIDs(got))
	}
}
