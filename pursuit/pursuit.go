// Package pursuit is what the user is actively pursuing: each application
// that isn't at a dead end, with its posting and its latest document
// reviews.
package pursuit

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// Application is an active application with its posting, company name and
// latest document review per document type.
type Application struct {
	store.Application
	Posting       store.Posting
	CompanyName   string
	LatestReviews map[documents.Type]docrecord.Review
	// StatusSince is when the application entered its current status.
	StatusSince time.Time
}

// List returns the active applications, most-recently-changed first.
//
// Reviews take one more read per application rather than a windowed join:
// active applications are a personal-scale list (a handful to a few
// dozen), so the extra local round trips are negligible.
func List(ctx context.Context, s *store.Store) ([]Application, error) {
	active, err := s.ListActiveApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("pursuit: list: %w", err)
	}
	apps := make([]Application, len(active))
	for i, a := range active {
		reviews, err := docrecord.LatestReviews(ctx, s.DB(), a.ID)
		if err != nil {
			return nil, fmt.Errorf("pursuit: reviews of application %d: %w", a.ID, err)
		}
		apps[i] = Application{
			Application:   a.Application,
			Posting:       a.Posting,
			CompanyName:   a.CompanyName,
			LatestReviews: reviews,
			StatusSince:   a.StatusSince,
		}
	}
	return apps, nil
}
