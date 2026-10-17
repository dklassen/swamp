package posting

import "time"

// Form is the application form a posting asks for, as stored: JSON, so
// the domain needn't know the job boards' form shape.
type Form struct {
	PostingID int64
	JSON      string
	FetchedAt time.Time
}
