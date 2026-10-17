package posting

import "github.com/dklassen/swamp/internal/domain/application"

// Interested is a posting the user has flagged interested and not
// archived, together with its company name and -- if the user (or an
// agent) has already started one -- its application's id and status.
// Unlike Posting, this always comes from a join rather than a single
// table, so it's kept as its own type instead of extending Posting with
// fields most callers don't need.
type Interested struct {
	Posting           Posting
	CompanyName       string
	ApplicationID     *int64
	ApplicationStatus *application.Status
}
