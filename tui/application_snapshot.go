package tui

import (
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/tui/message"
)

var statusSnapshots = map[application.Status]message.ApplicationStatus{
	application.Started:       message.ApplicationStarted,
	application.Submitted:     message.ApplicationSubmitted,
	application.Interviewing:  message.ApplicationInterviewing,
	application.Rejected:      message.ApplicationRejected,
	application.OfferReceived: message.ApplicationOfferReceived,
	application.OfferAccepted: message.ApplicationOfferAccepted,
	application.OfferDeclined: message.ApplicationOfferDeclined,
	application.PostingClosed: message.ApplicationPostingClosed,
	application.Withdrawn:     message.ApplicationWithdrawn,
}

func applicationSnapshot(a application.Application) message.Application {
	return message.Application{
		ID:        a.ID,
		PostingID: a.PostingID,
		Status:    statusSnapshots[a.Status],
		Notes:     a.Notes,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
		DeletedAt: a.DeletedAt,
	}
}

// applicationOf is the application a snapshot is of, for screens that
// still hold application.Application.
func applicationOf(s message.Application) application.Application {
	a := application.Application{
		ID:        s.ID,
		PostingID: s.PostingID,
		Notes:     s.Notes,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		DeletedAt: s.DeletedAt,
	}
	for status, snapshot := range statusSnapshots {
		if snapshot == s.Status {
			a.Status = status
		}
	}
	return a
}
