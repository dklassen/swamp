package store

import (
	"context"

	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/store/sqlcdb"
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
	ApplicationStatus *applicationstatus.Status
}

// interestedPostingFromRow converts a row built by sqlc.embed(postings) --
// row.Posting is already the exact nested sqlcdb.Posting that macro
// generates, so this runs it through the same PostingFromRow every other
// Posting goes through, rather than hand-reconstructing it field-by-field
// from a flat row (see ActiveApplication). The application
// side stays hand-checked (sql.NullInt64/sql.NullString .Valid) since
// it's LEFT JOINed and genuinely optional -- sqlc.embed(applications)
// isn't used here (see the query comment).
func interestedPostingFromRow(row sqlcdb.ListInterestedPostingsRow) (InterestedPosting, error) {
	p := InterestedPosting{
		Posting:     PostingFromRow(row.Posting),
		CompanyName: row.CompanyName,
	}
	if row.ApplicationID.Valid {
		id := row.ApplicationID.Int64
		p.ApplicationID = &id
	}
	if row.ApplicationStatus.Valid {
		status, err := applicationstatus.Parse(row.ApplicationStatus.String)
		if err != nil {
			return InterestedPosting{}, err
		}
		p.ApplicationStatus = &status
	}
	return p, nil
}

// ListInterestedPostings returns postings with interested_at set and
// archived_at unset, each joined with its company name and, if one
// exists, its application's id/status. Postings whose application is at
// a terminal status (see applicationstatus.Terminal) are excluded.
func (s *Store) ListInterestedPostings(ctx context.Context) ([]InterestedPosting, error) {
	rows, err := s.queries.ListInterestedPostings(ctx, terminalStatusParams())
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
