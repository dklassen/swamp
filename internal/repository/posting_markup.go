package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// FromPostingMarkupRow converts a generated posting_markup row.
func FromPostingMarkupRow(row sqlcdb.PostingMarkup) domain.Markup {
	m := domain.Markup{
		PostingID: row.PostingID,
		Notes:     row.Notes,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.InterestedAt.Valid {
		m.InterestedAt = &row.InterestedAt.Time
	}
	if row.ArchivedAt.Valid {
		m.ArchivedAt = &row.ArchivedAt.Time
	}
	return m
}

func readMarkup(ctx context.Context, r store.Runner, op string, fn func(*sqlcdb.Queries) (sqlcdb.PostingMarkup, error)) (*domain.Markup, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return nil, fmt.Errorf("postingmarkup: %s: %w", op, err)
	}
	m := FromPostingMarkupRow(row)
	return &m, nil
}

type PostingMarkupRepository struct{}

// Insert creates postingID's markup row, with nothing marked.
func (repository *PostingMarkupRepository) Insert(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "create", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.CreatePostingMarkup(ctx, postingID)
	})
}

// Get returns postingID's markup, or store.ErrNotFound.
func (repository *PostingMarkupRepository) Get(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "get", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.GetPostingMarkup(ctx, postingID)
	})
}

// GetMany returns the markup of the given postings that have it, in no
// order.
func (repository *PostingMarkupRepository) GetMany(ctx context.Context, r store.Runner, postingIDs []int64) ([]domain.Markup, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.PostingMarkup, error) {
		return q.ListPostingMarkupByPostingID(ctx, postingIDs)
	})
	if err != nil {
		return nil, fmt.Errorf("postingmarkup: get many: %w", err)
	}
	markup := make([]domain.Markup, len(rows))
	for i, row := range rows {
		markup[i] = FromPostingMarkupRow(row)
	}
	return markup, nil
}

// SetInterested sets InterestedAt and also clears ArchivedAt -- see the
// SetPostingInterested query comment for why.
func (repository *PostingMarkupRepository) SetInterested(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "set interested", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.SetPostingInterested(ctx, postingID)
	})
}

func (repository *PostingMarkupRepository) UnmarkInterested(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "unmark interested", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UnmarkPostingInterested(ctx, postingID)
	})
}

// SetArchived sets ArchivedAt and also clears InterestedAt -- see the
// SetPostingArchived query comment for why.
func (repository *PostingMarkupRepository) SetArchived(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "set archived", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.SetPostingArchived(ctx, postingID)
	})
}

func (repository *PostingMarkupRepository) Unarchive(ctx context.Context, r store.Runner, postingID int64) (*domain.Markup, error) {
	return readMarkup(ctx, r, "unarchive", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UnarchivePosting(ctx, postingID)
	})
}

func (repository *PostingMarkupRepository) SetNotes(ctx context.Context, r store.Runner, postingID int64, notes string) (*domain.Markup, error) {
	return readMarkup(ctx, r, "update notes", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UpdatePostingMarkupNotes(ctx, sqlcdb.UpdatePostingMarkupNotesParams{PostingID: postingID, Notes: notes})
	})
}
