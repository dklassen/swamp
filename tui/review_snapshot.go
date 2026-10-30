package tui

import (
	"slices"

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

// withReview is reviews with r in place of any earlier review of its
// document.
func withReview(reviews []message.Review, r message.Review) []message.Review {
	kept := slices.DeleteFunc(slices.Clone(reviews), func(old message.Review) bool { return old.DocumentType == r.DocumentType })
	return append(kept, r)
}

// documentTypeOf is the document type a snapshot names.
func documentTypeOf(s message.DocumentType) document.Type {
	for documentType, snapshot := range documentTypeSnapshots {
		if snapshot == s {
			return documentType
		}
	}
	return 0
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
		r := document.Review{ID: s.ID, ApplicationID: s.ApplicationID, DocumentType: documentTypeOf(s.DocumentType), Notes: s.Notes, CreatedAt: s.CreatedAt}
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
		ID:            s.ID,
		PostingID:     s.PostingID,
		Score:         s.Score,
		Reason:        s.Reason,
		Model:         s.Model,
		ClientName:    s.ClientName,
		ClientVersion: s.ClientVersion,
		ContentSHA256: s.ContentSHA256,
		ScoredAt:      s.ScoredAt,
	}
}

func documentWriteSnapshot(w document.Write) message.DocumentWrite {
	return message.DocumentWrite{
		ID:            w.ID,
		ApplicationID: w.ApplicationID,
		DocumentType:  documentTypeSnapshots[w.DocumentType],
		ContentSHA256: w.ContentSHA256,
		WrittenAt:     w.WrittenAt,
	}
}

func documentExportSnapshot(e document.Export) message.DocumentExport {
	return message.DocumentExport{
		ID:            e.ID,
		ApplicationID: e.ApplicationID,
		DocumentType:  documentTypeSnapshots[e.DocumentType],
		ContentSHA256: e.ContentSHA256,
		ExportedAt:    e.ExportedAt,
	}
}
