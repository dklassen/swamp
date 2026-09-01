package store

import (
	"context"

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
	Posting     Posting
	CompanyName string
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
// dead-end status (rejected, offer_declined), each viewed with its
// posting and company name resolved, most-recently-changed first.
func (s *Store) ListActiveApplications(ctx context.Context) ([]ApplicationView, error) {
	rows, err := s.queries.ListActiveApplications(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]ApplicationView, len(rows))
	for i, row := range rows {
		v, err := applicationViewFromRow(row)
		if err != nil {
			return nil, err
		}
		views[i] = v
	}
	return views, nil
}
