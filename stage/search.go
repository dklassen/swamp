package stage

import (
	"context"
	"fmt"
	"slices"

	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/store"
)

// Search's page sizes: what it returns when the caller doesn't say, and
// the most it ever returns (RFC 0006).
const (
	defaultSearchLimit = 50
	maxSearchLimit     = 100
)

// SortIDAsc orders matches by posting ID, oldest to Swamp first: the
// default, and so far the only order (#221).
const SortIDAsc = "id_asc"

// SearchOptions narrows Search. Every filter is optional; the zero value
// is every open, non-archived posting, in ID order, one page of
// defaultSearchLimit.
type SearchOptions struct {
	// CompanyID is one company, from Companies; 0 is any.
	CompanyID int64
	// HasApplication and Interested filter when set; nil is either.
	HasApplication      *bool
	Interested          *bool
	ApplicationStatuses []store.ApplicationStatus
	// ListingStatus is "open" (the default when empty), "closed" or "any".
	ListingStatus   string
	IncludeArchived bool
	// Sort is SortIDAsc, the default when empty.
	Sort string
	// Cursor is the previous page's NextCursor, passed back unchanged
	// with the same filters and Sort; empty starts from the beginning.
	Cursor string
	// Limit is the page size: defaultSearchLimit when 0 or less, never
	// more than maxSearchLimit. It isn't a filter, so it may change from
	// page to page.
	Limit int
}

// SearchResult is one page of Search's matches.
type SearchResult struct {
	Postings []SearchMatch `json:"Postings"`
	// NextCursor is what to pass as Cursor for the next page; nil on the
	// last page.
	NextCursor *string `json:"NextCursor"`
	// Total is how many postings match in all, when this page was read:
	// a hint for whether to narrow the search, not a promise about later
	// pages.
	Total int `json:"Total"`
}

// SearchMatch is one posting Search found: list_postings' summary, its
// company, listing status and markup, and its application, if any.
type SearchMatch struct {
	Posting           PostingSummary           `json:"Posting"`
	CompanyName       string                   `json:"CompanyName"`
	ListingStatus     string                   `json:"ListingStatus"`
	Interested        bool                     `json:"Interested"`
	Archived          bool                     `json:"Archived"`
	ApplicationID     *int64                   `json:"ApplicationID"`
	ApplicationStatus *store.ApplicationStatus `json:"ApplicationStatus"`
	ApplicationNotes  string                   `json:"ApplicationNotes"`
}

// searchFilters is what a cursor is bound to: the filters after defaults
// are applied, with statuses sorted, so the same search fingerprints the
// same however its options were written.
type searchFilters struct {
	CompanyID           int64
	HasApplication      *bool
	Interested          *bool
	ApplicationStatuses []string
	ListingStatus       string
	IncludeArchived     bool
}

// Search finds postings by what they are -- company, application,
// interest, listing status -- across every company the user hasn't
// deleted (#221, RFC 0006): how the agent resolves a posting or
// application the user names, which list_postings' drafting queue often
// leaves out. Pages are keyset pages behind opaque cursors, so they stay
// stable while other postings change.
func (st *Stage) Search(ctx context.Context, opts SearchOptions) (SearchResult, error) {
	listingStatus := opts.ListingStatus
	switch listingStatus {
	case "":
		listingStatus = "open"
	case "open", "closed", "any":
	default:
		return SearchResult{}, fmt.Errorf("stage: unknown listing status %q: want open, closed or any", opts.ListingStatus)
	}
	sort := opts.Sort
	if sort == "" {
		sort = SortIDAsc
	}
	if sort != SortIDAsc {
		return SearchResult{}, fmt.Errorf("stage: unknown sort %q: want %s", opts.Sort, SortIDAsc)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	filters := searchFilters{
		CompanyID:       opts.CompanyID,
		HasApplication:  opts.HasApplication,
		Interested:      opts.Interested,
		ListingStatus:   listingStatus,
		IncludeArchived: opts.IncludeArchived,
	}
	for _, status := range opts.ApplicationStatuses {
		filters.ApplicationStatuses = append(filters.ApplicationStatuses, status.String())
	}
	slices.Sort(filters.ApplicationStatuses)
	fingerprint := cursor.Fingerprint(filters)

	search := store.PostingSearch{
		CompanyID:           opts.CompanyID,
		IncludeArchived:     opts.IncludeArchived,
		HasApplication:      opts.HasApplication,
		Interested:          opts.Interested,
		ApplicationStatuses: opts.ApplicationStatuses,
		Limit:               limit,
	}
	if listingStatus != "any" {
		search.ListingStatus = listingStatus
	}
	if opts.Cursor != "" {
		key, err := cursor.Decode(opts.Cursor, sort, fingerprint)
		if err != nil {
			return SearchResult{}, fmt.Errorf("stage: %w", err)
		}
		search.AfterID = key.ID
	}

	page, err := st.store.SearchPostings(ctx, search)
	if err != nil {
		return SearchResult{}, fmt.Errorf("stage: %w", err)
	}
	result := SearchResult{Postings: make([]SearchMatch, len(page.Listings)), Total: page.Total}
	for i, l := range page.Listings {
		result.Postings[i] = searchMatch(l)
	}
	if page.HasMore {
		next := cursor.Encode(cursor.Key{Sort: sort, Filters: fingerprint, ID: page.Listings[len(page.Listings)-1].ID})
		result.NextCursor = &next
	}
	return result, nil
}

func searchMatch(l store.PostingListing) SearchMatch {
	match := SearchMatch{
		Posting: PostingSummary{
			ID:             l.ID,
			Title:          l.Title,
			Department:     l.Department,
			Location:       l.Location,
			WorkplaceType:  l.WorkplaceType,
			ApplicationURL: l.ApplicationURL,
		},
		CompanyName:   l.CompanyName,
		ListingStatus: l.ListingStatus,
		Interested:    l.Interested,
		Archived:      l.Archived,
	}
	if l.ApplicationID != 0 {
		id, status := l.ApplicationID, l.ApplicationStatus
		match.ApplicationID = &id
		match.ApplicationStatus = &status
		match.ApplicationNotes = l.ApplicationNotes
	}
	return match
}
