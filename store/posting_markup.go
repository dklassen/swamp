package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// PostingMarkup is the user's lightweight triage on a posting itself
// (interested/archived, independently togglable -- not a mutually
// exclusive pipeline) plus free-text notes -- deliberately separate from
// Application, which tracks the pursuit of the posting once the user
// actually starts applying (see Application). One row per posting, always
// present (see UpsertPosting).
type PostingMarkup struct {
	PostingID    int64
	InterestedAt *time.Time
	ArchivedAt   *time.Time
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func postingMarkupFromRow(row db.PostingMarkup) PostingMarkup {
	m := PostingMarkup{
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

func (s *Store) UnmarkPostingInterested(ctx context.Context, postingID int64) (PostingMarkup, error) {
	row, err := s.queries.UnmarkPostingInterested(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}

// SetPostingInterested sets InterestedAt and also clears ArchivedAt --
// see the SetPostingInterested query comment for why.
func (s *Store) SetPostingInterested(ctx context.Context, postingID int64) (PostingMarkup, error) {
	row, err := s.queries.SetPostingInterested(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}

func (s *Store) UnarchivePosting(ctx context.Context, postingID int64) (PostingMarkup, error) {
	row, err := s.queries.UnarchivePosting(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PostingMarkup{}, ErrNotFound
		}
		return PostingMarkup{}, err
	}
	return postingMarkupFromRow(row), nil
}

// SetPostingArchived is like ArchivePosting but also clears InterestedAt
// -- see the SetPostingArchived query comment for why.
func (s *Store) SetPostingArchived(ctx context.Context, postingID int64) (PostingMarkup, error) {
	row, err := s.queries.SetPostingArchived(ctx, postingID)
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
