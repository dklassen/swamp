package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/sqlcdb"
)

// ActiveApplication is an application that isn't at a dead end, with its
// posting and company name. pursuit adds its document reviews.
type ActiveApplication struct {
	Application
	Posting     Posting
	CompanyName string
	// StatusSince is when the application entered its current status, from
	// its newest status history row, or CreatedAt if it has none.
	StatusSince time.Time
}

// activeApplicationFromRow converts a row built by sqlc.embed(applications)/
// sqlc.embed(postings) -- row.Application and row.Posting are already the
// exact nested db types those macros generate, so this just runs them
// through the same conversion every other Application/Posting goes
// through, rather than hand-reconstructing them field-by-field from a
// flat row.
func activeApplicationFromRow(row sqlcdb.ListActiveApplicationsRow) (ActiveApplication, error) {
	app, err := applicationFromRow(row.Application)
	if err != nil {
		return ActiveApplication{}, err
	}
	return ActiveApplication{
		Application: app,
		Posting:     postingFromRow(row.Posting),
		CompanyName: row.CompanyName,
	}, nil
}

// ListActiveApplications returns the applications not at a terminal status
// (see applicationstatus.Terminal), each with its posting and company
// name, most-recently-changed first.
func (s *Store) ListActiveApplications(ctx context.Context) ([]ActiveApplication, error) {
	rows, err := s.queries.ListActiveApplications(ctx, terminalStatusParams())
	if err != nil {
		return nil, err
	}
	views := make([]ActiveApplication, len(rows))
	for i, row := range rows {
		v, err := activeApplicationFromRow(row)
		if err != nil {
			return nil, err
		}
		v.StatusSince, err = s.statusSince(ctx, v.Application)
		if err != nil {
			return nil, err
		}
		views[i] = v
	}
	return views, nil
}

// statusSince is when application entered its current status: its
// newest status history row, or CreatedAt for an application with no
// history (which only a write outside store could leave).
func (s *Store) statusSince(ctx context.Context, application Application) (time.Time, error) {
	row, err := s.queries.LatestApplicationStatusChange(ctx, application.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.CreatedAt, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("store: latest status change for application %d: %w", application.ID, err)
	}
	return row.ChangedAt, nil
}
