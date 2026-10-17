package posting

import "time"

// Markup is the user's lightweight triage on a posting itself
// (interested/archived, independently togglable -- not a mutually
// exclusive pipeline) plus free-text notes -- deliberately separate from
// an application, which tracks the pursuit of the posting once the user
// actually starts applying. One row per posting, always present: ingest
// creates it with the posting.
type Markup struct {
	PostingID    int64
	InterestedAt *time.Time
	ArchivedAt   *time.Time
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
