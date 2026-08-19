package store

import (
	"context"
	"database/sql"
	"errors"
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
// the DB's plain-TEXT status column into the typed ApplicationStatus enum
// (see application_status.go) -- this is the one place that parse can
// fail, since the DB itself no longer enforces which strings are valid.
func applicationFromRow(row db.Application) (Application, error) {
	status, err := ParseApplicationStatus(row.Status)
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

func (s *Store) CreateApplication(ctx context.Context, postingID int64) (Application, error) {
	row, err := s.queries.CreateApplication(ctx, postingID)
	if err != nil {
		return Application{}, err
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

func (s *Store) UpdateApplicationStatus(ctx context.Context, postingID int64, status ApplicationStatus) (Application, error) {
	row, err := s.queries.UpdateApplicationStatus(ctx, db.UpdateApplicationStatusParams{
		PostingID: postingID,
		Status:    status.String(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
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
