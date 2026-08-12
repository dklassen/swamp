package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// PostingMarkup is the user's own markup on a posting: status, free-text
// notes. One row per posting, always present (see UpsertPosting).
type PostingMarkup struct {
	PostingID  int64
	UserStatus string
	Notes      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func postingMarkupFromRow(row db.PostingMarkup) PostingMarkup {
	return PostingMarkup{
		PostingID:  row.PostingID,
		UserStatus: row.UserStatus,
		Notes:      row.Notes,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (s *Store) GetPostingMarkup(ctx context.Context, postingID int64) (PostingMarkup, error) {
	row, err := s.queries.GetPostingMarkup(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}

func (s *Store) UpdatePostingMarkupStatus(ctx context.Context, postingID int64, status string) (PostingMarkup, error) {
	row, err := s.queries.UpdatePostingMarkupStatus(ctx, db.UpdatePostingMarkupStatusParams{
		PostingID:  postingID,
		UserStatus: status,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}

func (s *Store) UpdatePostingMarkupNotes(ctx context.Context, postingID int64, notes string) (PostingMarkup, error) {
	row, err := s.queries.UpdatePostingMarkupNotes(ctx, db.UpdatePostingMarkupNotesParams{
		PostingID: postingID,
		Notes:     notes,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}
