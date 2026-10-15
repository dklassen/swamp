// Package postingmarkup is the user's triage on a posting: interested,
// archived, and notes.
package postingmarkup

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// Markup is the user's lightweight triage on a posting itself
// (interested/archived, independently togglable -- not a mutually
// exclusive pipeline) plus free-text notes -- deliberately separate from
// an application, which tracks the pursuit of the posting once the user
// actually starts applying. One row per posting, always present: ingest
// creates it with the posting.
type Markup struct {
	PostingID    int64
	InterestedAt *time.Time
	ArchivedAt   *time.Time
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func fromRow(row sqlcdb.PostingMarkup) Markup {
	m := Markup{
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

func run(ctx context.Context, r store.Runner, op string, fn func(*sqlcdb.Queries) (sqlcdb.PostingMarkup, error)) (Markup, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return Markup{}, fmt.Errorf("postingmarkup: %s: %w", op, err)
	}
	return fromRow(row), nil
}

// Get returns postingID's markup, or store.ErrNotFound.
func Get(ctx context.Context, r store.Runner, postingID int64) (Markup, error) {
	return run(ctx, r, "get", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.GetPostingMarkup(ctx, postingID)
	})
}

func UnmarkInterested(ctx context.Context, r store.Runner, postingID int64) (Markup, error) {
	return run(ctx, r, "unmark interested", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UnmarkPostingInterested(ctx, postingID)
	})
}

// SetInterested sets InterestedAt and also clears ArchivedAt -- see the
// SetPostingInterested query comment for why.
func SetInterested(ctx context.Context, r store.Runner, postingID int64) (Markup, error) {
	return run(ctx, r, "set interested", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.SetPostingInterested(ctx, postingID)
	})
}

func Unarchive(ctx context.Context, r store.Runner, postingID int64) (Markup, error) {
	return run(ctx, r, "unarchive", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UnarchivePosting(ctx, postingID)
	})
}

// SetArchived sets ArchivedAt and also clears InterestedAt -- see the
// SetPostingArchived query comment for why.
func SetArchived(ctx context.Context, r store.Runner, postingID int64) (Markup, error) {
	return run(ctx, r, "set archived", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.SetPostingArchived(ctx, postingID)
	})
}

func UpdateNotes(ctx context.Context, r store.Runner, postingID int64, notes string) (Markup, error) {
	return run(ctx, r, "update notes", func(q *sqlcdb.Queries) (sqlcdb.PostingMarkup, error) {
		return q.UpdatePostingMarkupNotes(ctx, sqlcdb.UpdatePostingMarkupNotesParams{PostingID: postingID, Notes: notes})
	})
}
