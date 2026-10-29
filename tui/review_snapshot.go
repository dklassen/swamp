package tui

import (
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/tui/message"
)

var documentTypeSnapshots = map[document.Type]message.DocumentType{
	document.CoverLetter: message.DocumentCoverLetter,
	document.Resume:      message.DocumentResume,
}

var outcomeSnapshots = map[document.Outcome]message.ReviewOutcome{
	document.Passed:  message.ReviewPassed,
	document.Flagged: message.ReviewFlagged,
}

func reviewSnapshot(r document.Review) message.Review {
	return message.Review{
		ID:            r.ID,
		ApplicationID: r.ApplicationID,
		DocumentType:  documentTypeSnapshots[r.DocumentType],
		Outcome:       outcomeSnapshots[r.Outcome],
		Notes:         r.Notes,
		CreatedAt:     r.CreatedAt,
	}
}

// reviewsOf is a review list by document type, for the screens that still
// render document.Review. Each has only the fields the snapshot carries.
func reviewsOf(list message.ReviewList) map[document.Type]document.Review {
	reviews := make(map[document.Type]document.Review, len(list.Reviews))
	for _, s := range list.Reviews {
		r := document.Review{ID: s.ID, ApplicationID: s.ApplicationID, Notes: s.Notes, CreatedAt: s.CreatedAt}
		for documentType, snapshot := range documentTypeSnapshots {
			if snapshot == s.DocumentType {
				r.DocumentType = documentType
			}
		}
		for outcome, snapshot := range outcomeSnapshots {
			if snapshot == s.Outcome {
				r.Outcome = outcome
			}
		}
		reviews[r.DocumentType] = r
	}
	return reviews
}

func scoreSnapshot(s score.Score) message.Score {
	return message.Score{
		Score:         s.Score,
		Reason:        s.Reason,
		Model:         s.Model,
		ClientName:    s.ClientName,
		ClientVersion: s.ClientVersion,
		ContentSHA256: s.ContentSHA256,
		ScoredAt:      s.ScoredAt,
	}
}
