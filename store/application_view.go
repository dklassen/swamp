package store

import (
	"context"
	"database/sql"

	"github.com/dklassen/swamp/store/db"
)

// ApplicationView is Application together with its posting and company
// name resolved -- a read-only view for screens that need that context
// without already having the posting in scope. Every GetApplication/
// CreateApplication/etc. caller already has the posting loaded (they're
// invoked from a screen that got there via a specific posting); this
// exists for cross-cutting screens (see ListActiveApplications) that
// don't. Embeds Application rather than duplicating its fields under a
// different name, so it's honestly the same entity with more resolved,
// not a second, partial representation of it (see decisions.log).
type ApplicationView struct {
	Application
	Posting       Posting
	CompanyName   string
	LatestReviews map[DocumentType]DocumentReview
}

// applicationViewFromRow converts a row built by sqlc.embed(applications)/
// sqlc.embed(postings) -- row.Application and row.Posting are already the
// exact nested db types those macros generate, so this just runs them
// through the same conversion every other Application/Posting goes
// through, rather than hand-reconstructing them field-by-field from a
// flat row (see decisions.log).
func applicationViewFromRow(row db.ListActiveApplicationsRow) (ApplicationView, error) {
	app, err := applicationFromRow(row.Application)
	if err != nil {
		return ApplicationView{}, err
	}
	return ApplicationView{
		Application: app,
		Posting:     postingFromRow(row.Posting),
		CompanyName: row.CompanyName,
	}, nil
}

// ListActiveApplications returns applications not at a terminal
// dead-end status (see TerminalApplicationStatuses), each viewed with
// its posting, company name, and latest document review per document
// type resolved, most-recently-changed first.
//
// LatestReviews is filled in with one extra query pair (cover letter,
// resume) per application rather than a single windowed join -- active
// applications are a personal-scale list (a handful to a few dozen, not
// thousands), so the extra local sqlite round trips are negligible, and
// reusing LatestDocumentReview keeps this in step with its own
// single-review semantics rather than duplicating that logic in SQL.
func (s *Store) ListActiveApplications(ctx context.Context) ([]ApplicationView, error) {
	terminal := TerminalApplicationStatuses()
	excluded := make([]sql.NullString, len(terminal))
	for i, status := range terminal {
		excluded[i] = sql.NullString{String: status.String(), Valid: true}
	}
	rows, err := s.queries.ListActiveApplications(ctx, excluded)
	if err != nil {
		return nil, err
	}
	views := make([]ApplicationView, len(rows))
	for i, row := range rows {
		v, err := applicationViewFromRow(row)
		if err != nil {
			return nil, err
		}
		v.LatestReviews, err = s.LatestDocumentReviews(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		views[i] = v
	}
	return views, nil
}
