package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dklassen/swamp/internal/domain/application"
	domain "github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type PostingSearchRepository struct{}

// Search returns one page of the postings matching search, in
// search.Order, after search.AfterID (#218, #223, RFC 0006). Paging by ID
// stays stable while other postings change: IDs never change, only
// increase and aren't reused, since nothing deletes postings. The
// filtering and the limit happen in the database, so a call never reads
// more than search.Limit + 1 rows. Postings of a company the user deleted
// are never included.
func (repository *PostingSearchRepository) Search(ctx context.Context, r store.Runner, search domain.Search) (domain.SearchPage, error) {
	if search.Limit <= 0 {
		return domain.SearchPage{}, fmt.Errorf("posting: search: limit %d, want at least 1", search.Limit)
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
		return domain.SearchPage{}, fmt.Errorf("posting: search: unknown listing status %q", search.ListingStatus)
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
			return domain.SearchPage{}, fmt.Errorf("posting: search: encode application statuses: %w", err)
		}
		params.Statuses = string(encoded)
	}
	if terms := strings.Fields(search.Query); len(terms) > 0 {
		encoded, err := json.Marshal(terms)
		if err != nil {
			return domain.SearchPage{}, fmt.Errorf("posting: search: encode query terms: %w", err)
		}
		params.Terms = string(encoded)
	}
	if search.AfterID != 0 {
		params.AfterID = search.AfterID
	}

	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.SearchPostingsByIDRow, error) {
		return searchInOrder(ctx, q, search, params)
	})
	if err != nil {
		return domain.SearchPage{}, fmt.Errorf("posting: search: %w", err)
	}
	page := domain.SearchPage{}
	if len(rows) > search.Limit {
		page.HasMore = true
		rows = rows[:search.Limit]
	}
	page.Listings = make([]domain.Listing, len(rows))
	for i, row := range rows {
		page.Total = int(row.Total)
		listing := domain.Listing{
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
			status, err := application.ParseStatus(row.ApplicationStatus.String)
			if err != nil {
				return domain.SearchPage{}, err
			}
			listing.ApplicationID = row.ApplicationID.Int64
			listing.ApplicationStatus = status
			listing.ApplicationNotes = row.ApplicationNotes.String
		}
		page.Listings[i] = listing
	}
	return page, nil
}

// searchInOrder runs order's query. Every order's query takes the same
// filters and returns the same columns; the generated param and row types
// differ only in name (and the cursor parameter's), so they convert
// directly.
func searchInOrder(ctx context.Context, q *sqlcdb.Queries, search domain.Search, params sqlcdb.SearchPostingsByIDParams) ([]sqlcdb.SearchPostingsByIDRow, error) {
	switch search.Order {
	case domain.OrderIDAsc:
		return q.SearchPostingsByID(ctx, params)
	case domain.OrderIDDesc:
		rows, err := q.SearchPostingsByIDDesc(ctx, sqlcdb.SearchPostingsByIDDescParams{
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
	case domain.OrderPublishedDesc:
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
		rows, err := q.SearchPostingsByPublishedDesc(ctx, values)
		converted := make([]sqlcdb.SearchPostingsByIDRow, len(rows))
		for i, row := range rows {
			converted[i] = sqlcdb.SearchPostingsByIDRow(row)
		}
		return converted, err
	case domain.OrderScoreDesc:
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
		rows, err := q.SearchPostingsByScoreDesc(ctx, values)
		converted := make([]sqlcdb.SearchPostingsByIDRow, len(rows))
		for i, row := range rows {
			converted[i] = sqlcdb.SearchPostingsByIDRow(row)
		}
		return converted, err
	default:
		return nil, fmt.Errorf("unknown sort order %s", search.Order)
	}
}
