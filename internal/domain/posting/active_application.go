package posting

import (
	"time"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
)

// ActiveApplication is an application not at a dead end, with its posting,
// company name and latest document review per document type.
type ActiveApplication struct {
	application.Application
	Posting       Posting
	CompanyName   string
	LatestReviews map[document.Type]document.Review
	// StatusSince is when the application entered its current status.
	StatusSince time.Time
}
