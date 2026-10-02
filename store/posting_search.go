package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dklassen/swamp/store/db"
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
	ApplicationURL string
	ListingStatus  string
	CompanyName    string
	Interested     bool
	Archived       bool
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
	// AfterID is the last posting ID of the previous page; 0 starts from
	// the beginning.
	AfterID int64
	Limit   int
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
// posting ID order, after search.AfterID (#218, RFC 0006). Paging by ID
// stays stable while other postings change: IDs never change, only
// increase and aren't reused, since nothing deletes postings. The
// filtering and the limit happen in the database, so a call never reads
// more than search.Limit + 1 rows. Postings of a company the user deleted are
// never included.
func (s *Store) SearchPostings(ctx context.Context, search PostingSearch) (PostingSearchPage, error) {
	if search.Limit <= 0 {
		return PostingSearchPage{}, fmt.Errorf("store: search postings: limit %d, want at least 1", search.Limit)
	}
	params := db.SearchPostingsByIDParams{
		IncludeArchived: search.IncludeArchived,
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
	if search.AfterID != 0 {
		params.AfterID = search.AfterID
	}

	rows, err := s.queries.SearchPostingsByID(ctx, params)
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
			ApplicationURL: row.ApplicationUrl,
			ListingStatus:  row.ListingStatus,
			CompanyName:    row.CompanyName,
			Interested:     row.InterestedAt.Valid,
			Archived:       row.ArchivedAt.Valid,
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
