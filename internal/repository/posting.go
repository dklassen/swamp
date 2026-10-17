package repository

import (
	"context"
	"database/sql"

	"github.com/dklassen/swamp/internal/domain/application"
	domain "github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type PostingRepository struct{}

// FromPostingRow converts a generated postings row.
func FromPostingRow(row sqlcdb.Posting) domain.Posting {
	return domain.Posting{
		ID:            row.ID,
		CompanyID:     row.CompanyID,
		Source:        row.Source,
		SourceID:      row.SourceID,
		ContentSHA256: row.ContentSha256,
		ListingStatus: row.ListingStatus,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		IngestedFields: domain.IngestedFields{
			Title:           row.Title,
			Department:      row.Department,
			Team:            row.Team,
			Location:        row.Location,
			EmploymentType:  row.EmploymentType,
			WorkplaceType:   row.WorkplaceType,
			DescriptionHTML: row.DescriptionHtml,
			DescriptionText: row.DescriptionText,
			JobURL:          row.JobUrl,
			ApplicationURL:  row.ApplicationUrl,
			PublishedAt:     domain.OptionalTime{Time: row.PublishedAt.Time},
			RawPayload:      row.RawPayload,
		},
	}
}

func readPosting(ctx context.Context, r store.Runner, fn func(*sqlcdb.Queries) (sqlcdb.Posting, error)) (domain.Posting, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return domain.Posting{}, err
	}
	return FromPostingRow(row), nil
}

// nullPublishedAt maps the zero PublishedAt, which means the board didn't
// say, to NULL.
func nullPublishedAt(t domain.OptionalTime) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.Time, Valid: true}
}

// Insert adds a posting fetched from source.
func (repository *PostingRepository) Insert(ctx context.Context, r store.Runner, companyID int64, source, sourceID string, fields domain.IngestedFields) (domain.Posting, error) {
	return readPosting(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Posting, error) {
		return q.CreatePosting(ctx, sqlcdb.CreatePostingParams{
			CompanyID:       companyID,
			Source:          source,
			SourceID:        sourceID,
			Title:           fields.Title,
			Department:      fields.Department,
			Team:            fields.Team,
			Location:        fields.Location,
			EmploymentType:  fields.EmploymentType,
			WorkplaceType:   fields.WorkplaceType,
			DescriptionHtml: fields.DescriptionHTML,
			DescriptionText: fields.DescriptionText,
			JobUrl:          fields.JobURL,
			ApplicationUrl:  fields.ApplicationURL,
			PublishedAt:     nullPublishedAt(fields.PublishedAt),
			RawPayload:      fields.RawPayload,
			ContentSha256:   domain.ContentSHA256(fields),
		})
	})
}

// Update replaces a posting's content. Its listing_status is left alone
// (see the UpdatePosting query).
func (repository *PostingRepository) Update(ctx context.Context, r store.Runner, id int64, fields domain.IngestedFields) (domain.Posting, error) {
	return readPosting(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Posting, error) {
		return q.UpdatePosting(ctx, sqlcdb.UpdatePostingParams{
			ID:              id,
			Title:           fields.Title,
			Department:      fields.Department,
			Team:            fields.Team,
			Location:        fields.Location,
			EmploymentType:  fields.EmploymentType,
			WorkplaceType:   fields.WorkplaceType,
			DescriptionHtml: fields.DescriptionHTML,
			DescriptionText: fields.DescriptionText,
			JobUrl:          fields.JobURL,
			ApplicationUrl:  fields.ApplicationURL,
			PublishedAt:     nullPublishedAt(fields.PublishedAt),
			RawPayload:      fields.RawPayload,
			ContentSha256:   domain.ContentSHA256(fields),
		})
	})
}

// Get returns a posting by ID, or store.ErrNotFound.
func (repository *PostingRepository) Get(ctx context.Context, r store.Runner, id int64) (domain.Posting, error) {
	return readPosting(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Posting, error) {
		return q.GetPosting(ctx, id)
	})
}

// GetBySource returns the posting source knows as sourceID, or
// store.ErrNotFound.
func (repository *PostingRepository) GetBySource(ctx context.Context, r store.Runner, source, sourceID string) (domain.Posting, error) {
	return readPosting(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Posting, error) {
		return q.GetPostingBySourceAndSourceID(ctx, sqlcdb.GetPostingBySourceAndSourceIDParams{Source: source, SourceID: sourceID})
	})
}

// ContentSHA256 returns a posting's current content hash, or
// store.ErrNotFound.
func (repository *PostingRepository) ContentSHA256(ctx context.Context, r store.Runner, id int64) (string, error) {
	return store.Read(ctx, r, func(q *sqlcdb.Queries) (string, error) {
		return q.GetPostingContentSHA256(ctx, id)
	})
}

func (repository *PostingRepository) ListByCompany(ctx context.Context, r store.Runner, companyID int64) ([]domain.Posting, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Posting, error) {
		return q.ListPostingsByCompany(ctx, companyID)
	})
	if err != nil {
		return nil, err
	}
	postings := make([]domain.Posting, len(rows))
	for i, row := range rows {
		postings[i] = FromPostingRow(row)
	}
	return postings, nil
}

// ListInterested returns postings with interested_at set and archived_at
// unset that the company's list shows, each with its company name and, if
// one exists, its application's id/status. Postings whose application is
// at one of excluding are left out.
func (repository *PostingRepository) ListInterested(ctx context.Context, r store.Runner, excluding []application.Status) ([]domain.Interested, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ListInterestedPostingsRow, error) {
		return q.ListInterestedPostings(ctx, statusParams(excluding))
	})
	if err != nil {
		return nil, err
	}
	postings := make([]domain.Interested, len(rows))
	for i, row := range rows {
		p := domain.Interested{
			Posting:     FromPostingRow(row.Posting),
			CompanyName: row.CompanyName,
		}
		if row.ApplicationID.Valid {
			id := row.ApplicationID.Int64
			p.ApplicationID = &id
		}
		if row.ApplicationStatus.Valid {
			status, err := application.ParseStatus(row.ApplicationStatus.String)
			if err != nil {
				return nil, err
			}
			p.ApplicationStatus = &status
		}
		postings[i] = p
	}
	return postings, nil
}

// statusParams converts statuses into the sqlc.slice argument the queries
// that exclude applications by status take.
func statusParams(statuses []application.Status) []sql.NullString {
	params := make([]sql.NullString, len(statuses))
	for i, status := range statuses {
		params[i] = sql.NullString{String: status.String(), Valid: true}
	}
	return params
}

func (repository *PostingRepository) ListOpenIDsByCompany(ctx context.Context, r store.Runner, companyID int64) ([]int64, error) {
	return store.Read(ctx, r, func(q *sqlcdb.Queries) ([]int64, error) {
		return q.ListOpenPostingIDsByCompany(ctx, companyID)
	})
}

// MarkSeen records that a sync just saw these postings on their board, by
// setting last_seen_at. It isn't a content change, so updated_at is left
// alone. No IDs is a no-op.
func (repository *PostingRepository) MarkSeen(ctx context.Context, r store.Runner, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.MarkPostingsSeen(ctx, ids)
	})
}

// Close closes an open posting, returning false when it was already
// closed.
func (repository *PostingRepository) Close(ctx context.Context, r store.Runner, id int64) (bool, error) {
	n, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.ClosePostingIfOpen(ctx, id)
	})
	return n > 0, err
}

// Reopen reopens a closed posting, returning false when it was already
// open.
func (repository *PostingRepository) Reopen(ctx context.Context, r store.Runner, id int64) (bool, error) {
	n, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.ReopenPostingIfClosed(ctx, id)
	})
	return n > 0, err
}

// Departments returns the distinct, non-empty department values across a
// company's postings -- the keyspace to offer when picking department
// filter values, since department is a company-specific vocabulary, not a
// fixed enum.
func (repository *PostingRepository) Departments(ctx context.Context, r store.Runner, companyID int64) ([]string, error) {
	return store.Read(ctx, r, func(q *sqlcdb.Queries) ([]string, error) {
		return q.ListDistinctDepartmentsForCompany(ctx, companyID)
	})
}

// Locations is Departments for location values.
func (repository *PostingRepository) Locations(ctx context.Context, r store.Runner, companyID int64) ([]string, error) {
	return store.Read(ctx, r, func(q *sqlcdb.Queries) ([]string, error) {
		return q.ListDistinctLocationsForCompany(ctx, companyID)
	})
}

// CountOpenByCompany returns, per company id, how many postings the
// company's posting list shows by default: still open on the job board,
// not archived, and let through by its filters. Deleted companies count
// nothing. Companies with none are absent from the map.
func (repository *PostingRepository) CountOpenByCompany(ctx context.Context, r store.Runner) (map[int64]int, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.CountOpenPostingsByCompanyRow, error) {
		return q.CountOpenPostingsByCompany(ctx)
	})
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(rows))
	for _, row := range rows {
		counts[row.CompanyID] = int(row.OpenPostings)
	}
	return counts, nil
}
