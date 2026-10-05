package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// Application is the user's own pursuit of a posting: status, free-text
// notes. Separate from PostingMarkup (posting-level triage) because a
// posting and an application have independent lifecycles -- a posting can
// be open/closed regardless of whether anyone applied. Unlike
// PostingMarkup, not every posting has one; it's created only once the
// user starts applying. One row per posting (see CreateApplication).
//
// Has its own surrogate ID (not PostingID reused as the identity, unlike
// PostingMarkup) because, unlike PostingMarkup, Application is itself
// referenced by InterviewStage -- reusing PostingID as the identity would
// make InterviewStage.ApplicationID hold posting IDs under an
// application-ID name.
type Application struct {
	ID        int64
	PostingID int64
	Status    ApplicationStatus
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// applicationFromRow converts a raw sqlc row into an Application, parsing
// the DB's status column into the typed ApplicationStatus enum (see
// application_status.go). status is nullable at the DB layer now (see
// db/migrations/00004_..., PR #17 review -- the DB no longer invents or
// enforces an initial value, the application does), but every write this
// package makes always supplies a concrete status; an actual NULL here
// means something outside this package wrote the row. The explicit Valid
// check below is technically redundant with ParseApplicationStatus
// rejecting "" (NullString's zero value) on its own, but it names the
// failure and includes the row id, which is worth the extra line for
// something that should never legitimately happen.
func applicationFromRow(row db.Application) (Application, error) {
	if !row.Status.Valid {
		return Application{}, fmt.Errorf("store: application %d has NULL status", row.ID)
	}
	status, err := ParseApplicationStatus(row.Status.String)
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
	}, nil
}

// CreateApplication starts an application for a posting and records its
// application_started status history row in the same transaction (#162).
// It refuses a closed posting with ErrPostingClosed (#175), checked in the
// same transaction so a sync closing the posting can't slip in between.
func (s *Store) CreateApplication(ctx context.Context, postingID int64) (Application, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Application{}, fmt.Errorf("store: begin create application tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)
	posting, err := qtx.GetPosting(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, fmt.Errorf("store: get posting to apply to: %w", err)
	}
	if posting.ListingStatus == "closed" {
		return Application{}, ErrPostingClosed
	}
	row, err := qtx.CreateApplication(ctx, db.CreateApplicationParams{
		PostingID: postingID,
		Status:    sql.NullString{String: ApplicationStatusStarted.String(), Valid: true},
	})
	if err != nil {
		return Application{}, err
	}
	if err := recordApplicationStatus(ctx, qtx, row.ID, ApplicationStatusStarted, StatusChangedByUser); err != nil {
		return Application{}, err
	}

	if err := tx.Commit(); err != nil {
		return Application{}, fmt.Errorf("store: commit create application tx: %w", err)
	}
	return applicationFromRow(row)
}

func (s *Store) GetApplication(ctx context.Context, postingID int64) (Application, error) {
	row, err := s.queries.GetApplication(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	return applicationFromRow(row)
}

// GetApplicationByID looks an application up by its own ID, unlike
// GetApplication and friends which all key off a postingID. Returns
// ErrNotFound for an ID with no row, matching GetApplication (see
// decisions.log, issue #102).
func (s *Store) GetApplicationByID(ctx context.Context, id int64) (Application, error) {
	row, err := s.queries.GetApplicationByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	return applicationFromRow(row)
}

// UpdateApplicationStatus sets a posting's application status and records
// the change in its status history in the same transaction (#162). Saving
// the status it already has records nothing: it isn't a change.
func (s *Store) UpdateApplicationStatus(ctx context.Context, postingID int64, status ApplicationStatus) (Application, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Application{}, fmt.Errorf("store: begin update application status tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)
	before, err := qtx.GetApplication(ctx, postingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	row, err := qtx.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
		PostingID: postingID,
		Status:    sql.NullString{String: status.String(), Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	if before.Status.String != status.String() {
		if err := recordApplicationStatus(ctx, qtx, row.ID, status, StatusChangedByUser); err != nil {
			return Application{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Application{}, fmt.Errorf("store: commit update application status tx: %w", err)
	}
	return applicationFromRow(row)
}

func (s *Store) UpdateApplicationNotes(ctx context.Context, postingID int64, notes string) (Application, error) {
	row, err := s.queries.UpdateApplicationNotes(ctx, db.UpdateApplicationNotesParams{
		PostingID: postingID,
		Notes:     notes,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	return applicationFromRow(row)
}

// DeleteApplication removes an application outright (#232), for one
// started by accident, along with every row it owns, in one transaction.
// The posting and its markup are untouched. It's a hard delete, not a
// soft one like companies and tags: nothing else refers to an
// application, and SQLite reuses the highest rowid, so a leftover row
// would attach itself to the next application created. It returns
// ErrNotFound for an ID with no row. The application's documents on
// disk are the caller's to remove (see documents.Store.RemoveDir).
func (s *Store) DeleteApplication(ctx context.Context, id int64) error {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin delete application tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)
	for name, deleteOwned := range map[string]func(context.Context, int64) error{
		"status history":   qtx.DeleteApplicationStatusHistory,
		"document reviews": qtx.DeleteDocumentReviewsForApplication,
		"document exports": qtx.DeleteDocumentExportsForApplication,
		"interview stages": qtx.DeleteInterviewStagesForApplication,
	} {
		if err := deleteOwned(ctx, id); err != nil {
			return fmt.Errorf("store: delete application %s: %w", name, err)
		}
	}
	n, err := qtx.DeleteApplication(ctx, id)
	if err != nil {
		return fmt.Errorf("store: delete application: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit delete application tx: %w", err)
	}
	return nil
}
