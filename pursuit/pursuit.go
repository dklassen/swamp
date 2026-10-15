// Package pursuit is what the user is actively pursuing: each application
// that isn't at a dead end, with its posting and its latest document
// reviews.
package pursuit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// Application is an active application with its posting, company name and
// latest document review per document type.
type Application struct {
	application.Application
	Posting       store.Posting
	CompanyName   string
	LatestReviews map[documents.Type]docrecord.Review
	// StatusSince is when the application entered its current status.
	StatusSince time.Time
}

// List returns the applications not at a terminal status (see
// applicationstatus.Terminal), most-recently-changed first.
//
// Status-since and reviews take a few more reads per application rather
// than a windowed join: active applications are a personal-scale list (a
// handful to a few dozen), so the extra local round trips are negligible.
func List(ctx context.Context, r store.Runner) ([]Application, error) {
	terminal := applicationstatus.Terminal()
	params := make([]sql.NullString, len(terminal))
	for i, status := range terminal {
		params[i] = sql.NullString{String: status.String(), Valid: true}
	}
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ListActiveApplicationsRow, error) {
		return q.ListActiveApplications(ctx, params)
	})
	if err != nil {
		return nil, fmt.Errorf("pursuit: list: %w", err)
	}
	apps := make([]Application, len(rows))
	for i, row := range rows {
		app, err := application.FromRow(row.Application)
		if err != nil {
			return nil, fmt.Errorf("pursuit: %w", err)
		}
		since, err := application.StatusSince(ctx, r, app)
		if err != nil {
			return nil, fmt.Errorf("pursuit: %w", err)
		}
		reviews, err := docrecord.LatestReviews(ctx, r, app.ID)
		if err != nil {
			return nil, fmt.Errorf("pursuit: reviews of application %d: %w", app.ID, err)
		}
		apps[i] = Application{
			Application:   app,
			Posting:       store.PostingFromRow(row.Posting),
			CompanyName:   row.CompanyName,
			LatestReviews: reviews,
			StatusSince:   since,
		}
	}
	return apps, nil
}
