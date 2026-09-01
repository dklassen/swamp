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

func applicationViewFromRow(row db.ListActiveApplicationsRow) (ApplicationView, error) {
	app, err := applicationFromRow(db.Application{
		ID:        row.ApplicationID,
		PostingID: row.ID,
		Status:    row.ApplicationStatus,
		Notes:     row.ApplicationNotes,
		CreatedAt: row.ApplicationCreatedAt,
		UpdatedAt: row.ApplicationUpdatedAt,
	})
	if err != nil {
		return ApplicationView{}, err
	}
	return ApplicationView{
		Application: app,
		Posting: postingFromRow(db.Posting{
			ID:              row.ID,
			CompanyID:       row.CompanyID,
			Source:          row.Source,
			SourceID:        row.SourceID,
			Title:           row.Title,
			Department:      row.Department,
			Team:            row.Team,
			Location:        row.Location,
			EmploymentType:  row.EmploymentType,
			WorkplaceType:   row.WorkplaceType,
			DescriptionHtml: row.DescriptionHtml,
			DescriptionText: row.DescriptionText,
			JobUrl:          row.JobUrl,
			ApplicationUrl:  row.ApplicationUrl,
			PublishedAt:     row.PublishedAt,
			RawPayload:      row.RawPayload,
			ListingStatus:   row.ListingStatus,
			FirstSeenAt:     row.FirstSeenAt,
			LastSeenAt:      row.LastSeenAt,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
		}),
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
