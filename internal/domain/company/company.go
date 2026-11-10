// Package company is a company whose job board Swamp syncs.
package company

import "time"

type Company struct {
	ID          int64
	Name        string
	Source      string
	SourceRef   string
	Description string
	// LastFetchedAt is when this company's postings were last fetched
	// successfully; the zero value means never.
	LastFetchedAt time.Time
	DeletedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Stats is a company with how many postings its list shows:
// open, unarchived and let through by its filters.
type Stats struct {
	Company

	OpenPostings int
}
