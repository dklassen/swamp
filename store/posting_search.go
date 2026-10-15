package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dklassen/swamp/store/sqlcdb"
)

// PostingListing is one posting in a search's results: its summary
// fields, its company, its markup flags and its application, if any
// (RFC 0006). No description or raw payload (#117).
type PostingListing struct {
	ID             int64
	Title          string
	Department     string
	Location       string
	WorkplaceType  string
	JobURL         string
	ApplicationURL string
	// PublishedAt is when the board says the posting was published; zero
	// when it didn't say.
	PublishedAt time.Time
	// FirstSeenAt is when Swamp first stored the posting.
	FirstSeenAt   time.Time
	ListingStatus string
	CompanyName   string
	Interested    bool
	Archived      bool
	// Score and ScoreReason are the posting's current score, its newest;
	// Score is 0 when it has none, since scores start at 1.
	Score       int
	ScoreReason string
	// ScoreStale is whether the content has changed since it was scored.
	ScoreStale bool
	// ApplicationID is 0 when the posting has no application; the other
	// Application fields are meaningless then.
	ApplicationID     int64
	ApplicationStatus ApplicationStatus
	ApplicationNotes  string
}

// PostingSearch is one page's worth of SearchPostings. Every filter is
// optional: its zero value doesn't filter. Limit must be positive.
type PostingSearch struct {
	// CompanyID is one company's ID; 0 is any company.
	CompanyID int64
	// ListingStatus is "open" or "closed"; "" is either.
	ListingStatus   string
	IncludeArchived bool
	// HasApplication and Interested filter when set; nil is either.
	HasApplication      *bool
	Interested          *bool
	ApplicationStatuses []ApplicationStatus
	// Query is space-separated terms, each of which must appear in the
	// posting's title, company, department or location, ignoring case.
	// "" matches every posting.
	Query string
	// IgnoreCompanyFilters includes postings their company's filters
	// hide; by default a search shows what the company's list shows.
	IgnoreCompanyFilters bool
	// Order is the sort order; the zero value is PostingOrderIDAsc.
	Order PostingOrder
	// AfterID is the last posting ID of the previous page: the next page
	// is the rows after it in Order. 0 starts from the beginning.
	AfterID int64
	// AfterPublishedAt is that row's PublishedAt, for
	// PostingOrderPublishedDesc; zero when it had none. Other orders
	// ignore it.
	AfterPublishedAt time.Time
	// AfterScore is that row's Score, for PostingOrderScoreDesc; 0 when it
	// had none. Other orders ignore it.
	AfterScore int
	Limit      int
}

// PostingOrder is a sort order SearchPostings pages in (#223, RFC 0006).
// Each ends with the posting ID, which never changes, so paging stays
// stable; each is its own static query, with the same filters.
type PostingOrder int

const (
	// PostingOrderIDAsc: the order Swamp first saw the postings in.
	PostingOrderIDAsc PostingOrder = iota
	// PostingOrderIDDesc: the postings Swamp saw most recently first.
	PostingOrderIDDesc
	// PostingOrderPublishedDesc: newest on the board first, by
	// published_at then ID, descending; postings with no published_at
	// last. Stable while paging unless a board changes a posting's
	// published_at mid-paging: that posting can cross the cursor.
	PostingOrderPublishedDesc
	// PostingOrderScoreDesc: best fit first, by current score, then newest
	// to Swamp among equal scores; unscored postings last.
	PostingOrderScoreDesc
)

// postingOrderNames is the single name table for PostingOrder, indexed by
// its value.
var postingOrderNames = [...]string{
	PostingOrderIDAsc:         "id_asc",
	PostingOrderIDDesc:        "id_desc",
	PostingOrderPublishedDesc: "published_desc",
	PostingOrderScoreDesc:     "score_desc",
}

func (o PostingOrder) String() string {
	if o < 0 || int(o) >= len(postingOrderNames) {
		return fmt.Sprintf("PostingOrder(%d)", int(o))
	}
	return postingOrderNames[o]
}

// PostingOrders returns every sort order, in const order.
func PostingOrders() []PostingOrder {
	orders := make([]PostingOrder, len(postingOrderNames))
	for i := range postingOrderNames {
		orders[i] = PostingOrder(i)
	}
	return orders
}

// PostingSearchPage is one page of SearchPostings' results.
type PostingSearchPage struct {
	Listings []PostingListing
	// HasMore is whether postings after this page match too. Following it
	// never leads to an empty page past the end.
	HasMore bool
	// Total is how many postings match the search in all, when the page
	// was read. It's 0 on an empty page: nothing matches, or nothing after
	// AfterID does any more.
	Total int
}

// SearchPostings returns one page of the postings matching search, in
// search.Order, after search.AfterID (#218, #223, RFC 0006). Paging by ID
// stays stable while other postings change: IDs never change, only
// increase and aren't reused, since nothing deletes postings. The
// filtering and the limit happen in the database, so a call never reads
// more than search.Limit + 1 rows. Postings of a company the user deleted are
// never included.
func (s *Store) SearchPostings(ctx context.Context, search PostingSearch) (PostingSearchPage, error) {
	if search.Limit <= 0 {
		return PostingSearchPage{}, fmt.Errorf("store: search postings: limit %d, want at least 1", search.Limit)
	}
	params := sqlcdb.SearchPostingsByIDParams{
		IncludeArchived:      search.IncludeArchived,
		IgnoreCompanyFilters: search.IgnoreCompanyFilters,
		// One more than asked for, to learn whether another page follows.
		MaxRows: int64(search.Limit) + 1,
	}
	switch search.ListingStatus {
	case "":
	case "open", "closed":
		params.ListingStatus = search.ListingStatus
	default:
		return PostingSearchPage{}, fmt.Errorf("store: search postings: unknown listing status %q", search.ListingStatus)
	}
	if search.CompanyID != 0 {
		params.CompanyID = search.CompanyID
	}
	if search.HasApplication != nil {
		params.HasApplication = *search.HasApplication
	}
	if search.Interested != nil {
		params.Interested = *search.Interested
	}
	if len(search.ApplicationStatuses) > 0 {
		names := make([]string, len(search.ApplicationStatuses))
		for i, status := range search.ApplicationStatuses {
			names[i] = status.String()
		}
		encoded, err := json.Marshal(names)
		if err != nil {
			return PostingSearchPage{}, fmt.Errorf("store: encode application statuses: %w", err)
		}
		params.Statuses = string(encoded)
	}
	if terms := strings.Fields(search.Query); len(terms) > 0 {
		encoded, err := json.Marshal(terms)
		if err != nil {
			return PostingSearchPage{}, fmt.Errorf("store: encode query terms: %w", err)
		}
		params.Terms = string(encoded)
	}
	if search.AfterID != 0 {
		params.AfterID = search.AfterID
	}

	rows, err := s.searchPostingsInOrder(ctx, search, params)
	if err != nil {
		return PostingSearchPage{}, fmt.Errorf("store: search postings: %w", err)
	}
	page := PostingSearchPage{}
	if len(rows) > search.Limit {
		page.HasMore = true
		rows = rows[:search.Limit]
	}
	page.Listings = make([]PostingListing, len(rows))
	for i, row := range rows {
		page.Total = int(row.Total)
		listing := PostingListing{
			ID:             row.ID,
			Title:          row.Title,
			Department:     row.Department,
			Location:       row.Location,
			WorkplaceType:  row.WorkplaceType,
			JobURL:         row.JobUrl,
			ApplicationURL: row.ApplicationUrl,
			PublishedAt:    row.PublishedAt.Time,
			FirstSeenAt:    row.FirstSeenAt,
			ListingStatus:  row.ListingStatus,
			CompanyName:    row.CompanyName,
			Interested:     row.InterestedAt.Valid,
			Archived:       row.ArchivedAt.Valid,
		}
		if row.CurrentScore.Valid {
			listing.Score = int(row.CurrentScore.Int64)
			listing.ScoreReason = row.CurrentScoreReason.String
			listing.ScoreStale = row.CurrentScoreContentSha256.String != row.ContentSha256
		}
		if row.ApplicationID.Valid {
			status, err := ParseApplicationStatus(row.ApplicationStatus.String)
			if err != nil {
				return PostingSearchPage{}, err
			}
			listing.ApplicationID = row.ApplicationID.Int64
			listing.ApplicationStatus = status
			listing.ApplicationNotes = row.ApplicationNotes.String
		}
		page.Listings[i] = listing
	}
	return page, nil
}

// searchPostingsInOrder runs order's query. Every order's query takes the
// same filters and returns the same columns; the generated param and row
// types differ only in name (and the cursor parameter's), so they convert
// directly.
func (s *Store) searchPostingsInOrder(ctx context.Context, search PostingSearch, params sqlcdb.SearchPostingsByIDParams) ([]sqlcdb.SearchPostingsByIDRow, error) {
	switch search.Order {
	case PostingOrderIDAsc:
		return s.queries.SearchPostingsByID(ctx, params)
	case PostingOrderIDDesc:
		rows, err := s.queries.SearchPostingsByIDDesc(ctx, sqlcdb.SearchPostingsByIDDescParams{
			CompanyID:            params.CompanyID,
			ListingStatus:        params.ListingStatus,
			IncludeArchived:      params.IncludeArchived,
			HasApplication:       params.HasApplication,
			Interested:           params.Interested,
			Statuses:             params.Statuses,
			Terms:                params.Terms,
			IgnoreCompanyFilters: params.IgnoreCompanyFilters,
			BeforeID:             params.AfterID,
			MaxRows:              params.MaxRows,
		})
		converted := make([]sqlcdb.SearchPostingsByIDRow, len(rows))
		for i, row := range rows {
			converted[i] = sqlcdb.SearchPostingsByIDRow(row)
		}
		return converted, err
	case PostingOrderPublishedDesc:
		values := sqlcdb.SearchPostingsByPublishedDescParams{
			CompanyID:            params.CompanyID,
			ListingStatus:        params.ListingStatus,
			IncludeArchived:      params.IncludeArchived,
			HasApplication:       params.HasApplication,
			Interested:           params.Interested,
			Statuses:             params.Statuses,
			Terms:                params.Terms,
			IgnoreCompanyFilters: params.IgnoreCompanyFilters,
			CursorID:             params.AfterID,
			MaxRows:              params.MaxRows,
		}
		if search.AfterID != 0 && !search.AfterPublishedAt.IsZero() {
			values.CursorPublished = search.AfterPublishedAt
		}
		rows, err := s.queries.SearchPostingsByPublishedDesc(ctx, values)
		converted := make([]sqlcdb.SearchPostingsByIDRow, len(rows))
		for i, row := range rows {
			converted[i] = sqlcdb.SearchPostingsByIDRow(row)
		}
		return converted, err
	case PostingOrderScoreDesc:
		values := sqlcdb.SearchPostingsByScoreDescParams{
			CompanyID:            params.CompanyID,
			ListingStatus:        params.ListingStatus,
			IncludeArchived:      params.IncludeArchived,
			HasApplication:       params.HasApplication,
			Interested:           params.Interested,
			Statuses:             params.Statuses,
			Terms:                params.Terms,
			IgnoreCompanyFilters: params.IgnoreCompanyFilters,
			CursorID:             params.AfterID,
			MaxRows:              params.MaxRows,
		}
		if search.AfterID != 0 && search.AfterScore != 0 {
			values.CursorScore = search.AfterScore
		}
		rows, err := s.queries.SearchPostingsByScoreDesc(ctx, values)
		converted := make([]sqlcdb.SearchPostingsByIDRow, len(rows))
		for i, row := range rows {
			converted[i] = sqlcdb.SearchPostingsByIDRow(row)
		}
		return converted, err
	default:
		return nil, fmt.Errorf("unknown sort order %s", search.Order)
	}
}
