package store

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/store/db"
)

// ActiveApplication is an application not at a terminal dead-end status
// (rejected, offer_declined), together with its posting and company name
// -- see ListActiveApplications. Unlike InterestedPosting, the
// application always exists here (this comes from an inner join), so
// ApplicationID/ApplicationStatus are concrete values, not pointers.
type ActiveApplication struct {
	Posting           Posting
	CompanyName       string
	ApplicationID     int64
	ApplicationStatus ApplicationStatus
}

func activeApplicationFromRow(row db.ListActiveApplicationsRow) (ActiveApplication, error) {
	if !row.ApplicationStatus.Valid {
		return ActiveApplication{}, fmt.Errorf("store: application %d has NULL status", row.ApplicationID)
	}
	status, err := ParseApplicationStatus(row.ApplicationStatus.String)
	if err != nil {
		return ActiveApplication{}, err
	}
	return ActiveApplication{
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
		CompanyName:       row.CompanyName,
		ApplicationID:     row.ApplicationID,
		ApplicationStatus: status,
	}, nil
}

// ListActiveApplications returns applications not at a terminal
// dead-end status (rejected, offer_declined), each joined with its
// posting and company name, most-recently-changed first.
func (s *Store) ListActiveApplications(ctx context.Context) ([]ActiveApplication, error) {
	rows, err := s.queries.ListActiveApplications(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]ActiveApplication, len(rows))
	for i, row := range rows {
		a, err := activeApplicationFromRow(row)
		if err != nil {
			return nil, err
		}
		apps[i] = a
	}
	return apps, nil
}
