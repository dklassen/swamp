package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// Posting is a source-agnostic job posting: canonical fields are
// normalized across boards, RawPayload preserves the original response for
// anything not (yet) promoted to a canonical field.
type Posting struct {
	ID              int64
	CompanyID       int64
	Source          string
	SourceID        string
	Title           string
	Department      *string
	Team            *string
	Location        *string
	EmploymentType  *string
	WorkplaceType   *string
	DescriptionHTML *string
	DescriptionText *string
	JobURL          *string
	ApplicationURL  *string
	PublishedAt     *time.Time
	RawPayload      string
	ListingStatus   string
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreatePostingParams are the ingested fields for a posting. This is the
// shape whoever maps a source-specific posting (e.g. ashby.Posting) into
// store is expected to populate; store itself has no knowledge of any
// specific job board.
type CreatePostingParams struct {
	CompanyID       int64
	Source          string
	SourceID        string
	Title           string
	Department      *string
	Team            *string
	Location        *string
	EmploymentType  *string
	WorkplaceType   *string
	DescriptionHTML *string
	DescriptionText *string
	JobURL          *string
	ApplicationURL  *string
	PublishedAt     *time.Time
	RawPayload      string
}

func postingFromRow(row db.Posting) Posting {
	p := Posting{
		ID:            row.ID,
		CompanyID:     row.CompanyID,
		Source:        row.Source,
		SourceID:      row.SourceID,
		Title:         row.Title,
		RawPayload:    row.RawPayload,
		ListingStatus: row.ListingStatus,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	if row.Department.Valid {
		p.Department = &row.Department.String
	}
	if row.Team.Valid {
		p.Team = &row.Team.String
	}
	if row.Location.Valid {
		p.Location = &row.Location.String
	}
	if row.EmploymentType.Valid {
		p.EmploymentType = &row.EmploymentType.String
	}
	if row.WorkplaceType.Valid {
		p.WorkplaceType = &row.WorkplaceType.String
	}
	if row.DescriptionHtml.Valid {
		p.DescriptionHTML = &row.DescriptionHtml.String
	}
	if row.DescriptionText.Valid {
		p.DescriptionText = &row.DescriptionText.String
	}
	if row.JobUrl.Valid {
		p.JobURL = &row.JobUrl.String
	}
	if row.ApplicationUrl.Valid {
		p.ApplicationURL = &row.ApplicationUrl.String
	}
	if row.PublishedAt.Valid {
		p.PublishedAt = &row.PublishedAt.Time
	}
	return p
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// UpsertPosting inserts a new posting or updates the existing one for the
// same (source, source_id), keeping the row's listing_status untouched on
// update (see UpdatePosting query comment for why).
//
// On insert, it also creates the posting's markup row, defaulted to
// user status new with empty notes, in the same transaction, so every
// posting always has exactly one markup row and callers never have to
// remember a second call to create it. This is deliberately not exposed
// as two separate Store calls.
func (s *Store) UpsertPosting(ctx context.Context, params CreatePostingParams) (Posting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Posting{}, fmt.Errorf("store: begin upsert posting tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)

	existing, err := qtx.GetPostingBySourceAndSourceID(ctx, db.GetPostingBySourceAndSourceIDParams{
		Source:   params.Source,
		SourceID: params.SourceID,
	})

	var row db.Posting
	switch {
	case errors.Is(err, sql.ErrNoRows):
		row, err = qtx.CreatePosting(ctx, db.CreatePostingParams{
			CompanyID:       params.CompanyID,
			Source:          params.Source,
			SourceID:        params.SourceID,
			Title:           params.Title,
			Department:      nullString(params.Department),
			Team:            nullString(params.Team),
			Location:        nullString(params.Location),
			EmploymentType:  nullString(params.EmploymentType),
			WorkplaceType:   nullString(params.WorkplaceType),
			DescriptionHtml: nullString(params.DescriptionHTML),
			DescriptionText: nullString(params.DescriptionText),
			JobUrl:          nullString(params.JobURL),
			ApplicationUrl:  nullString(params.ApplicationURL),
			PublishedAt:     nullTime(params.PublishedAt),
			RawPayload:      params.RawPayload,
		})
		if err != nil {
			return Posting{}, fmt.Errorf("store: create posting: %w", err)
		}
		if _, err := qtx.CreatePostingMarkup(ctx, row.ID); err != nil {
			return Posting{}, fmt.Errorf("store: create posting markup: %w", err)
		}
	case err != nil:
		return Posting{}, fmt.Errorf("store: get posting by source: %w", err)
	default:
		row, err = qtx.UpdatePosting(ctx, db.UpdatePostingParams{
			ID:              existing.ID,
			Title:           params.Title,
			Department:      nullString(params.Department),
			Team:            nullString(params.Team),
			Location:        nullString(params.Location),
			EmploymentType:  nullString(params.EmploymentType),
			WorkplaceType:   nullString(params.WorkplaceType),
			DescriptionHtml: nullString(params.DescriptionHTML),
			DescriptionText: nullString(params.DescriptionText),
			JobUrl:          nullString(params.JobURL),
			ApplicationUrl:  nullString(params.ApplicationURL),
			PublishedAt:     nullTime(params.PublishedAt),
			RawPayload:      params.RawPayload,
		})
		if err != nil {
			return Posting{}, fmt.Errorf("store: update posting: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Posting{}, fmt.Errorf("store: commit upsert posting tx: %w", err)
	}
	return postingFromRow(row), nil
}

func (s *Store) GetPosting(ctx context.Context, id int64) (Posting, error) {
	row, err := s.queries.GetPosting(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Posting{}, ErrNotFound
		}
		return Posting{}, err
	}
	return postingFromRow(row), nil
}

func (s *Store) GetPostingBySourceAndSourceID(ctx context.Context, source, sourceID string) (Posting, error) {
	row, err := s.queries.GetPostingBySourceAndSourceID(ctx, db.GetPostingBySourceAndSourceIDParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Posting{}, ErrNotFound
		}
		return Posting{}, err
	}
	return postingFromRow(row), nil
}

func (s *Store) ListPostingsByCompany(ctx context.Context, companyID int64) ([]Posting, error) {
	rows, err := s.queries.ListPostingsByCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	postings := make([]Posting, len(rows))
	for i, row := range rows {
		postings[i] = postingFromRow(row)
	}
	return postings, nil
}

// MarkPostingClosed marks a posting closed, e.g. because it no longer
// appeared in the most recent fetch of its company's board.
func (s *Store) MarkPostingClosed(ctx context.Context, id int64) error {
	return s.queries.MarkPostingClosed(ctx, id)
}

// MarkPostingReopened marks a previously-closed posting open again, e.g.
// because it reappeared in a fetch.
func (s *Store) MarkPostingReopened(ctx context.Context, id int64) error {
	return s.queries.MarkPostingReopened(ctx, id)
}

// ListDistinctDepartmentsForCompany returns the distinct, non-empty
// department values seen across a company's ingested postings -- the
// keyspace to offer when picking department filter values, since
// department is a company-specific vocabulary, not a fixed enum.
func (s *Store) ListDistinctDepartmentsForCompany(ctx context.Context, companyID int64) ([]string, error) {
	rows, err := s.queries.ListDistinctDepartmentsForCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Valid {
			values = append(values, r.String)
		}
	}
	return values, nil
}

// ListDistinctLocationsForCompany is ListDistinctDepartmentsForCompany
// for location values.
func (s *Store) ListDistinctLocationsForCompany(ctx context.Context, companyID int64) ([]string, error) {
	rows, err := s.queries.ListDistinctLocationsForCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Valid {
			values = append(values, r.String)
		}
	}
	return values, nil
}
