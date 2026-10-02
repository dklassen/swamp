package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// searchFixture is two live companies' postings, plus a deleted company's,
// covering every filter SearchPostings has. The postings are created in
// this order, so their IDs ascend in it.
type searchFixture struct {
	acme, beta                                     Company
	platform, data, designer, writer, shelf, other Posting
	platformApp, dataApp                           Application
}

func newSearchFixture(t *testing.T, s *Store) searchFixture {
	t.Helper()
	ctx := context.Background()
	f := searchFixture{
		acme: mustCreateCompany(t, s, "Acme", "ashby", "acme"),
		beta: mustCreateCompany(t, s, "Beta", "ashby", "beta"),
	}
	gone := mustCreateCompany(t, s, "Gone", "ashby", "gone")

	f.platform = mustUpsertPosting(t, s, f.acme.ID, "1", "Platform Engineer")
	f.data = mustUpsertPosting(t, s, f.acme.ID, "2", "Data Engineer")
	f.designer = mustUpsertPosting(t, s, f.acme.ID, "3", "Designer")
	f.writer = mustUpsertPosting(t, s, f.acme.ID, "4", "Writer")
	f.shelf = mustUpsertPosting(t, s, f.acme.ID, "5", "Shelved Role")
	f.other = mustUpsertPosting(t, s, f.beta.ID, "6", "Engineer")
	mustUpsertPosting(t, s, gone.ID, "7", "Deleted company's posting")

	if _, err := s.SetPostingInterested(ctx, f.platform.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	if _, err := s.SetPostingInterested(ctx, f.designer.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	f.platformApp = mustCreateApplication(t, s, f.platform.ID)
	f.dataApp = mustCreateApplication(t, s, f.data.ID)
	mustUpdateApplicationStatus(t, s, f.data.ID, ApplicationStatusSubmitted)
	if err := s.MarkPostingClosed(ctx, f.writer.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if _, err := s.SetPostingArchived(ctx, f.shelf.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	if err := s.SoftDeleteCompany(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	return f
}

func listingIDs(listings []PostingListing) []int64 {
	ids := make([]int64, len(listings))
	for i, l := range listings {
		ids[i] = l.ID
	}
	return ids
}

func TestSearchPostings_Filters(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	yes, no := true, false

	tests := []struct {
		name   string
		search PostingSearch
		want   []Posting
	}{
		{
			name:   "no filters: every posting of a live company, archived ones too",
			search: PostingSearch{IncludeArchived: true},
			want:   []Posting{f.platform, f.data, f.designer, f.writer, f.shelf, f.other},
		},
		{
			name: "archived left out unless asked for",
			want: []Posting{f.platform, f.data, f.designer, f.writer, f.other},
		},
		{
			name:   "company",
			search: PostingSearch{CompanyID: f.beta.ID},
			want:   []Posting{f.other},
		},
		{
			name:   "open",
			search: PostingSearch{ListingStatus: "open"},
			want:   []Posting{f.platform, f.data, f.designer, f.other},
		},
		{
			name:   "closed",
			search: PostingSearch{ListingStatus: "closed"},
			want:   []Posting{f.writer},
		},
		{
			name:   "with an application",
			search: PostingSearch{HasApplication: &yes},
			want:   []Posting{f.platform, f.data},
		},
		{
			name:   "without an application",
			search: PostingSearch{HasApplication: &no},
			want:   []Posting{f.designer, f.writer, f.other},
		},
		{
			name:   "application statuses",
			search: PostingSearch{ApplicationStatuses: []ApplicationStatus{ApplicationStatusSubmitted, ApplicationStatusWithdrawn}},
			want:   []Posting{f.data},
		},
		{
			name:   "interested",
			search: PostingSearch{Interested: &yes},
			want:   []Posting{f.platform, f.designer},
		},
		{
			name:   "not interested",
			search: PostingSearch{Interested: &no},
			want:   []Posting{f.data, f.writer, f.other},
		},
		{
			name:   "filters combine: interested at Acme with no application",
			search: PostingSearch{CompanyID: f.acme.ID, Interested: &yes, HasApplication: &no},
			want:   []Posting{f.designer},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.search.Limit = 100
			got, err := s.SearchPostings(context.Background(), tt.search)
			if err != nil {
				t.Fatalf("SearchPostings: %v", err)
			}
			want := make([]int64, len(tt.want))
			for i, p := range tt.want {
				want[i] = p.ID
			}
			if diff := cmp.Diff(want, listingIDs(got.Listings)); diff != "" {
				t.Errorf("IDs mismatch (-want +got):\n%s", diff)
			}
			if got.Total != len(tt.want) {
				t.Errorf("Total = %d, want %d", got.Total, len(tt.want))
			}
		})
	}
}

// TestSearchPostings_ListingFields: each listing carries the summary, the
// company name, the markup flags and the application, when there is one.
func TestSearchPostings_ListingFields(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	if _, err := s.UpdateApplicationNotes(context.Background(), f.data.ID, "heard back"); err != nil {
		t.Fatalf("UpdateApplicationNotes: %v", err)
	}

	got, err := s.SearchPostings(context.Background(), PostingSearch{CompanyID: f.acme.ID, IncludeArchived: true, Limit: 100})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	want := []PostingListing{
		{ID: f.platform.ID, Title: "Platform Engineer", ListingStatus: "open", CompanyName: "Acme", Interested: true,
			ApplicationID: f.platformApp.ID, ApplicationStatus: ApplicationStatusStarted},
		{ID: f.data.ID, Title: "Data Engineer", ListingStatus: "open", CompanyName: "Acme",
			ApplicationID: f.dataApp.ID, ApplicationStatus: ApplicationStatusSubmitted, ApplicationNotes: "heard back"},
		{ID: f.designer.ID, Title: "Designer", ListingStatus: "open", CompanyName: "Acme", Interested: true},
		{ID: f.writer.ID, Title: "Writer", ListingStatus: "closed", CompanyName: "Acme"},
		{ID: f.shelf.ID, Title: "Shelved Role", ListingStatus: "open", CompanyName: "Acme", Archived: true},
	}
	if diff := cmp.Diff(want, got.Listings); diff != "" {
		t.Errorf("listings mismatch (-want +got):\n%s", diff)
	}
}

// TestSearchPostings_Paging: pages read with each page's last ID as the
// next cursor, while HasMore says there are more, never exceed the limit,
// concatenate to the unpaged result, and report the whole result's size
// as Total on every page (#218). The last page says HasMore is false, so
// a caller never asks for an empty page past the end.
func TestSearchPostings_Paging(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	newSearchFixture(t, s)

	for _, order := range PostingOrders() {
		t.Run(order.String(), func(t *testing.T) {
			testPaging(t, s, order)
		})
	}
}

func testPaging(t *testing.T, s *Store, order PostingOrder) {
	t.Helper()
	ctx := context.Background()
	all, err := s.SearchPostings(ctx, PostingSearch{Order: order, IncludeArchived: true, Limit: 100})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	var paged []int64
	search := PostingSearch{Order: order, IncludeArchived: true, Limit: 4}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging didn't end")
		}
		page, err := s.SearchPostings(ctx, search)
		if err != nil {
			t.Fatalf("SearchPostings: %v", err)
		}
		if len(page.Listings) > search.Limit {
			t.Fatalf("page has %d listings, limit %d", len(page.Listings), search.Limit)
		}
		if page.Total != len(all.Listings) {
			t.Errorf("page Total = %d, want %d on every page", page.Total, len(all.Listings))
		}
		if len(page.Listings) == 0 {
			t.Fatal("empty page: the previous page should have said HasMore is false")
		}
		paged = append(paged, listingIDs(page.Listings)...)
		if !page.HasMore {
			break
		}
		search.AfterID = page.Listings[len(page.Listings)-1].ID
	}
	if diff := cmp.Diff(listingIDs(all.Listings), paged); diff != "" {
		t.Errorf("pages don't concatenate to the whole result (-want +got):\n%s", diff)
	}
}

// TestSearchPostings_ClosedMidPagingDoesNotShiftLaterPages: with keyset
// paging, a row leaving the result between two pages doesn't move any
// other row -- the second page is the same as if nothing had changed.
// With offset paging it would skip a row (RFC 0006).
func TestSearchPostings_ClosedMidPagingDoesNotShiftLaterPages(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	ctx := context.Background()
	search := PostingSearch{ListingStatus: "open", Limit: 2}

	first, err := s.SearchPostings(ctx, search)
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if diff := cmp.Diff([]int64{f.platform.ID, f.data.ID}, listingIDs(first.Listings)); diff != "" {
		t.Fatalf("first page mismatch (-want +got):\n%s", diff)
	}
	if err := s.MarkPostingClosed(ctx, f.platform.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	search.AfterID = first.Listings[len(first.Listings)-1].ID
	second, err := s.SearchPostings(ctx, search)
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if diff := cmp.Diff([]int64{f.designer.ID, f.other.ID}, listingIDs(second.Listings)); diff != "" {
		t.Errorf("second page shifted (-want +got):\n%s", diff)
	}
}

func TestSearchPostings_InvalidSearch(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	for name, search := range map[string]PostingSearch{
		"no limit":              {},
		"negative limit":        {Limit: -1},
		"unknown listing state": {ListingStatus: "pending", Limit: 10},
	} {
		if _, err := s.SearchPostings(context.Background(), search); err == nil {
			t.Errorf("%s: SearchPostings(%+v) = nil error, want one", name, search)
		}
	}
}

// TestSearchPostings_HasMore: false on the last page, including a page
// that exactly fills the limit, and when nothing matches.
func TestSearchPostings_HasMore(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	ctx := context.Background()

	for _, tt := range []struct {
		name     string
		search   PostingSearch
		wantRows int
		wantMore bool
	}{
		{"more than the limit", PostingSearch{CompanyID: f.acme.ID, Limit: 2}, 2, true},
		{"exactly the limit", PostingSearch{CompanyID: f.beta.ID, Limit: 1}, 1, false},
		{"nothing matches", PostingSearch{ListingStatus: "closed", CompanyID: f.beta.ID, Limit: 5}, 0, false},
	} {
		got, err := s.SearchPostings(ctx, tt.search)
		if err != nil {
			t.Fatalf("%s: SearchPostings: %v", tt.name, err)
		}
		if len(got.Listings) != tt.wantRows || got.HasMore != tt.wantMore {
			t.Errorf("%s: %d rows, HasMore %v; want %d, %v", tt.name, len(got.Listings), got.HasMore, tt.wantRows, tt.wantMore)
		}
	}
}

// TestSearchPostings_EveryOrderMatchesTheSameRows: each sort order is its
// own static query, repeating the filters (sqlc has no way to share
// them). This keeps the copies from drifting: for the same search, every
// order returns the same postings, only ordered differently (#223).
func TestSearchPostings_EveryOrderMatchesTheSameRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	yes, no := true, false

	for name, search := range map[string]PostingSearch{
		"everything":                 {IncludeArchived: true},
		"defaults":                   {},
		"company, closed":            {CompanyID: f.acme.ID, ListingStatus: "closed"},
		"applications by status":     {ApplicationStatuses: []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted}},
		"interested, no application": {Interested: &yes, HasApplication: &no},
	} {
		search.Limit = 100
		var want []int64
		for _, order := range PostingOrders() {
			search.Order = order
			got, err := s.SearchPostings(context.Background(), search)
			if err != nil {
				t.Fatalf("%s, %s: SearchPostings: %v", name, order, err)
			}
			sorted := slices.Sorted(slices.Values(listingIDs(got.Listings)))
			if want == nil {
				want = sorted
				continue
			}
			if diff := cmp.Diff(want, sorted); diff != "" {
				t.Errorf("%s: %s matches different postings than %s (-want +got):\n%s", name, order, PostingOrders()[0], diff)
			}
		}
	}
}

// TestSearchPostings_DescendingClosedMidPagingDoesNotShift: the keyset
// guarantee holds newest-first too.
func TestSearchPostings_DescendingClosedMidPagingDoesNotShift(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)
	ctx := context.Background()
	search := PostingSearch{Order: PostingOrderIDDesc, ListingStatus: "open", Limit: 2}

	first, err := s.SearchPostings(ctx, search)
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if diff := cmp.Diff([]int64{f.other.ID, f.designer.ID}, listingIDs(first.Listings)); diff != "" {
		t.Fatalf("first page mismatch (-want +got):\n%s", diff)
	}
	if err := s.MarkPostingClosed(ctx, f.other.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	search.AfterID = first.Listings[len(first.Listings)-1].ID
	second, err := s.SearchPostings(ctx, search)
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if diff := cmp.Diff([]int64{f.data.ID, f.platform.ID}, listingIDs(second.Listings)); diff != "" {
		t.Errorf("second page shifted (-want +got):\n%s", diff)
	}
}

// TestSearchPostings_IDDescIsNewestToSwampFirst: id_desc is the reverse
// of id_asc -- the postings Swamp saw most recently first (#223).
func TestSearchPostings_IDDescIsNewestToSwampFirst(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	f := newSearchFixture(t, s)

	got, err := s.SearchPostings(context.Background(), PostingSearch{Order: PostingOrderIDDesc, CompanyID: f.acme.ID, IncludeArchived: true, ListingStatus: "", Limit: 100})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	want := []int64{f.shelf.ID, f.writer.ID, f.designer.ID, f.data.ID, f.platform.ID}
	if diff := cmp.Diff(want, listingIDs(got.Listings)); diff != "" {
		t.Errorf("IDs mismatch (-want +got):\n%s", diff)
	}
}

// TestSearchPostings_PublishedDesc: newest on the board first (#224).
// Ties on published_at break by ID, descending; fractional seconds of
// any length compare by instant, as does a time given in another zone;
// postings with no published_at come last, newest ID first. Every page
// size pages through to the same order.
func TestSearchPostings_PublishedDesc(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	at := func(value string) OptionalTime {
		t.Helper()
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatalf("parse %s: %v", value, err)
		}
		return OptionalTime{Time: parsed}
	}
	posting := func(sourceID string, published OptionalTime) Posting {
		t.Helper()
		p, err := s.UpsertPosting(ctx, CreatePostingParams{
			CompanyID: acme.ID, Source: "ashby", SourceID: sourceID,
			IngestedFields: IngestedFields{Title: "Role " + sourceID, PublishedAt: published, RawPayload: "{}"},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
		return p
	}
	oldest := posting("1", at("2026-09-01T03:00:00-07:00")) // 10:00 UTC
	halfSecond := posting("2", at("2026-09-03T08:00:00.5Z"))
	fortyFive := posting("3", at("2026-09-03T08:00:00.45Z"))
	wholeSecond := posting("4", at("2026-09-03T08:00:00Z"))
	halfSecondTie := posting("5", at("2026-09-03T08:00:00.5Z"))
	undatedOld := posting("6", OptionalTime{})
	undatedNew := posting("7", OptionalTime{})
	want := []int64{halfSecondTie.ID, halfSecond.ID, fortyFive.ID, wholeSecond.ID, oldest.ID, undatedNew.ID, undatedOld.ID}

	for limit := 1; limit <= len(want); limit++ {
		search := PostingSearch{Order: PostingOrderPublishedDesc, Limit: limit}
		var got []int64
		for pages := 0; ; pages++ {
			if pages > len(want) {
				t.Fatalf("limit %d: paging didn't end", limit)
			}
			page, err := s.SearchPostings(ctx, search)
			if err != nil {
				t.Fatalf("limit %d: SearchPostings: %v", limit, err)
			}
			got = append(got, listingIDs(page.Listings)...)
			if !page.HasMore {
				break
			}
			last := page.Listings[len(page.Listings)-1]
			search.AfterID, search.AfterPublishedAt = last.ID, last.PublishedAt
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("limit %d: order mismatch (-want +got):\n%s", limit, diff)
		}
	}
}
