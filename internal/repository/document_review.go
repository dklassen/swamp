package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// FromDocumentReviewRow parses the DB's document_type/outcome columns into
// their typed enums -- the DB doesn't enforce either with a CHECK
// constraint, so this is where an unknown value is caught.
func FromDocumentReviewRow(row sqlcdb.DocumentReview) (domain.Review, error) {
	documentType, err := domain.ParseType(row.DocumentType)
	if err != nil {
		return domain.Review{}, err
	}
	outcome, err := domain.ParseOutcome(row.Outcome)
	if err != nil {
		return domain.Review{}, err
	}
	return domain.Review{
		ID:              row.ID,
		ApplicationID:   row.ApplicationID,
		DocumentType:    documentType,
		Cycle:           row.Cycle,
		ContentSnapshot: row.ContentSnapshot,
		ContentSHA256:   row.ContentSha256,
		Outcome:         outcome,
		Notes:           row.Notes,
		CreatedAt:       row.CreatedAt,
	}, nil
}

type DocumentReviewRepository struct{}

// Count returns how many reviews applicationID's documentType has.
func (repository *DocumentReviewRepository) Count(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type) (int64, error) {
	count, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.CountDocumentReviews(ctx, sqlcdb.CountDocumentReviewsParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if err != nil {
		return 0, fmt.Errorf("document: count reviews: %w", err)
	}
	return count, nil
}

// Insert records a review of content, documentType's content as of now,
// as the given cycle.
func (repository *DocumentReviewRepository) Insert(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type, cycle int64, content string, outcome domain.Outcome, notes string) (*domain.Review, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.DocumentReview, error) {
		return q.CreateDocumentReview(ctx, sqlcdb.CreateDocumentReviewParams{
			ApplicationID:   applicationID,
			DocumentType:    documentType.String(),
			Cycle:           cycle,
			ContentSnapshot: content,
			ContentSha256:   domain.ContentSHA256(content),
			Outcome:         outcome.String(),
			Notes:           notes,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("document: create review: %w", err)
	}
	review, err := FromDocumentReviewRow(row)
	if err != nil {
		return nil, err
	}
	return &review, nil
}

// List returns applicationID's reviews of documentType, most recent cycle
// first.
func (repository *DocumentReviewRepository) List(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type) ([]domain.Review, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.DocumentReview, error) {
		return q.ListDocumentReviews(ctx, sqlcdb.ListDocumentReviewsParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("document: list reviews: %w", err)
	}
	reviews := make([]domain.Review, len(rows))
	for i, row := range rows {
		if reviews[i], err = FromDocumentReviewRow(row); err != nil {
			return nil, err
		}
	}
	return reviews, nil
}
