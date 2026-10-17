package posting

import "time"

// The History.ChangeType values: what happened to the posting after its
// snapshot was taken.
const (
	ChangeContentUpdated = "content_updated"
	ChangeClosed         = "closed"
	ChangeReopened       = "reopened"
)

// History is a snapshot of a posting's prior ingested state, written
// whenever a re-fetch detects a diff in an ingested field, or the
// posting's listing_status transitions.
type History struct {
	ID         int64
	PostingID  int64
	ChangeType string
	Snapshot   string
	RecordedAt time.Time
}
