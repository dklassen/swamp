// Package application is the user's pursuit of a posting: its status and
// status history, notes, and the application form it asks for.
package application

import (
	"time"
)

// Application is the user's own pursuit of a posting: status, free-text
// notes. Separate from posting markup (posting-level triage) because a
// posting and an application have independent lifecycles -- a posting can
// be open/closed regardless of whether anyone applied. Unlike posting
// markup, not every posting has one; it's created only once the user
// starts applying. One row per posting (see Create).
//
// Has its own surrogate ID (not PostingID reused as the identity, unlike
// posting markup) because interview_stages references it.
type Application struct {
	ID        int64
	PostingID int64
	Status    Status
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt is when the user deleted the application, or zero while
	// it's live. Only GetByIDIncludingDeleted returns deleted ones.
	DeletedAt time.Time
}
