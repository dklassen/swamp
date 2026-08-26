package store

import (
	"context"

	"github.com/dklassen/swamp/store/db"
)

// InterestedPosting is a posting the user has flagged interested and not
// archived, together with its company name and -- if the user (or an
// agent) has already started one -- its application's id and status.
// Unlike Posting, this always comes from a join (see
// ListInterestedPostings) rather than a single table, so it's kept as its
// own type instead of extending Posting with fields most callers don't
// need.
type InterestedPosting struct {
	Posting           Posting
	CompanyName       string
	ApplicationID     *int64
	ApplicationStatus *ApplicationStatus
}

func interestedPostingFromRow(row db.ListInterestedPostingsRow) (InterestedPosting, error) {
	p := InterestedPosting{
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
	}
	if row.ApplicationID.Valid {
		id := row.ApplicationID.Int64
		p.ApplicationID = &id
	}
	if row.ApplicationStatus.Valid {
		status, err := ParseApplicationStatus(row.ApplicationStatus.String)
		if err != nil {
			return InterestedPosting{}, err
		}
		p.ApplicationStatus = &status
	}
	return p, nil
}

// ListInterestedPostings returns postings with interested_at set and
// archived_at unset, each joined with its company name and, if one
// exists, its application's id/status.
func (s *Store) ListInterestedPostings(ctx context.Context) ([]InterestedPosting, error) {
	rows, err := s.queries.ListInterestedPostings(ctx)
	if err != nil {
		return nil, err
	}
	postings := make([]InterestedPosting, len(rows))
	for i, row := range rows {
		p, err := interestedPostingFromRow(row)
		if err != nil {
			return nil, err
		}
		postings[i] = p
	}
	return postings, nil
}
