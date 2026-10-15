// Package application is the user's pursuit of a posting: its status and
// status history, notes, and the application form it asks for.
package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// ErrPostingClosed is Create's error for a posting that's no longer
// listed: its form can't be submitted, and sync only ends an application
// when its posting closes, so one started now would sit at
// application_started for good.
var ErrPostingClosed = errors.New("application: posting is closed")

// Application is the user's own pursuit of a posting: status, free-text
// notes. Separate from posting markup (posting-level triage) because a
// posting and an application have independent lifecycles -- a posting can
// be open/closed regardless of whether anyone applied. Unlike posting
// markup, not every posting has one; it's created only once the user
// starts applying. One row per posting (see Create).
//
// Has its own surrogate ID (not PostingID reused as the identity, unlike
// posting markup) because interview_stages references it.
type Application struct {
	ID        int64
	PostingID int64
	Status    applicationstatus.Status
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt is when the user deleted the application, or zero while
	// it's live. Only GetByIDIncludingDeleted returns deleted ones.
	DeletedAt time.Time
}

// FromRow converts a generated applications row, parsing its status. A
// NULL status means something outside Swamp wrote the row: every write
// here supplies one.
func FromRow(row sqlcdb.Application) (Application, error) {
	if !row.Status.Valid {
		return Application{}, fmt.Errorf("application: application %d has NULL status", row.ID)
	}
	status, err := applicationstatus.Parse(row.Status.String)
	if err != nil {
		return Application{}, err
	}
	return Application{
		ID:        row.ID,
		PostingID: row.PostingID,
		Status:    status,
		Notes:     row.Notes,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt.Time,
	}, nil
}

func read(ctx context.Context, r store.Runner, fn func(*sqlcdb.Queries) (sqlcdb.Application, error)) (Application, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return Application{}, err
	}
	return FromRow(row)
}

// Create starts an application for a posting and records its
// application_started status history row. It refuses a closed posting
// with ErrPostingClosed, checked in the same transaction so a sync closing
// the posting can't slip in between.
func Create(ctx context.Context, tx store.Tx, postingID int64) (Application, error) {
	var created Application
	err := store.InTx(ctx, tx, func(tx store.Tx) error {
		posting, err := store.Read(ctx, tx, func(q *sqlcdb.Queries) (sqlcdb.Posting, error) {
			return q.GetPosting(ctx, postingID)
		})
		if err != nil {
			return err
		}
		if posting.ListingStatus == "closed" {
			return ErrPostingClosed
		}
		created, err = read(ctx, tx, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
			return q.CreateApplication(ctx, sqlcdb.CreateApplicationParams{
				PostingID: postingID,
				Status:    sql.NullString{String: applicationstatus.Started.String(), Valid: true},
			})
		})
		if err != nil {
			return err
		}
		return recordStatus(ctx, tx, created.ID, applicationstatus.Started, ChangedByUser)
	})
	if err != nil {
		return Application{}, fmt.Errorf("application: create: %w", err)
	}
	return created, nil
}

// Get returns postingID's live application, or store.ErrNotFound.
func Get(ctx context.Context, r store.Runner, postingID int64) (Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplication(ctx, postingID)
	})
}

// GetByID looks an application up by its own ID, unlike Get, which keys
// off a posting ID. Returns store.ErrNotFound for an ID with no live row.
func GetByID(ctx context.Context, r store.Runner, id int64) (Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplicationByID(ctx, id)
	})
}

// GetByIDIncludingDeleted is GetByID, but also finds an application the
// user deleted (check DeletedAt). Returns store.ErrNotFound for an ID that
// was never handed out.
func GetByIDIncludingDeleted(ctx context.Context, r store.Runner, id int64) (Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplicationByIDIncludingDeleted(ctx, id)
	})
}

// UpdateStatus sets a posting's application status and records the change
// in its status history. Saving the status it already has records nothing:
// it isn't a change.
func UpdateStatus(ctx context.Context, tx store.Tx, postingID int64, status applicationstatus.Status) (Application, error) {
	var updated Application
	err := store.InTx(ctx, tx, func(tx store.Tx) error {
		before, err := Get(ctx, tx, postingID)
		if err != nil {
			return err
		}
		updated, err = setStatus(ctx, tx, postingID, status)
		if err != nil {
			return err
		}
		if before.Status == status {
			return nil
		}
		return recordStatus(ctx, tx, updated.ID, status, ChangedByUser)
	})
	if err != nil {
		return Application{}, fmt.Errorf("application: update status: %w", err)
	}
	return updated, nil
}

func setStatus(ctx context.Context, tx store.Tx, postingID int64, status applicationstatus.Status) (Application, error) {
	return read(ctx, tx, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.UpdateApplicationStatus(ctx, sqlcdb.UpdateApplicationStatusParams{
			PostingID: postingID,
			Status:    sql.NullString{String: status.String(), Valid: true},
		})
	})
}

func UpdateNotes(ctx context.Context, r store.Runner, postingID int64, notes string) (Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.UpdateApplicationNotes(ctx, sqlcdb.UpdateApplicationNotesParams{PostingID: postingID, Notes: notes})
	})
}

// Delete soft-deletes an application: it disappears from every lookup and
// list, and the posting can start a fresh one, but its status history,
// reviews, exports and interview stages stay, as do its documents on disk.
// It returns store.ErrNotFound for an ID with no live application.
func Delete(ctx context.Context, r store.Runner, id int64) error {
	n, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.SoftDeleteApplication(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("application: delete: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}
