package store

import (
	"context"
	"fmt"
)

// PostingListing is one posting in ListPostingListings: its summary
// fields, its company, its markup flags and its application, if any
// (#215). No description or raw payload (#117).
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

// ListPostingListings returns every posting of a company the user hasn't
// deleted, ordered by company name then title: what the agent's
// search_postings filters (stage.Search, #215).
func (s *Store) ListPostingListings(ctx context.Context) ([]PostingListing, error) {
	rows, err := s.queries.ListPostingListings(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list posting listings: %w", err)
	}
	listings := make([]PostingListing, len(rows))
	for i, row := range rows {
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
				return nil, err
			}
			listing.ApplicationID = row.ApplicationID.Int64
			listing.ApplicationStatus = status
			listing.ApplicationNotes = row.ApplicationNotes.String
		}
		listings[i] = listing
	}
	return listings, nil
}
