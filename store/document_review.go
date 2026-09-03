package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// Document type and review outcome values for DocumentReview.DocumentType/
// Outcome -- plain TEXT with a DB CHECK constraint (like
// interview_stages.outcome), not a Go enum: unlike applications.status
// (see decisions.log, 00004_drop_application_status_check_constraint),
// this is a small, stable set with no history of churn.
const (
	DocumentTypeCoverLetter = "cover_letter"
	DocumentTypeResume      = "resume"

	ReviewOutcomePassed  = "passed"
	ReviewOutcomeFlagged = "flagged"
)

// DocumentReview is one human pass over a drafted cover letter or resume,
// owned by the user and append-only -- never edited or deleted once
// created (see decisions.log, #51). ContentSnapshot captures the
// document's content as of the moment it was reviewed, since
// documents.Store overwrites cover_letter.md/resume.md in place with no
// versioning of its own: without a snapshot, a review would become
// unreadable the moment the file gets redrafted. ContentSHA256 is a
// derived shortcut for "did this change since the last review" without
// comparing full ContentSnapshot values.
type DocumentReview struct {
	ID              int64
	ApplicationID   int64
	DocumentType    string
	Cycle           int64
	ContentSnapshot string
	ContentSHA256   string
	Outcome         string
	Notes           string
	CreatedAt       time.Time
}

func documentReviewFromRow(row db.DocumentReview) DocumentReview {
	return DocumentReview{
		ID:              row.ID,
		ApplicationID:   row.ApplicationID,
		DocumentType:    row.DocumentType,
		Cycle:           row.Cycle,
		ContentSnapshot: row.ContentSnapshot,
		ContentSHA256:   row.ContentSha256,
		Outcome:         row.Outcome,
		Notes:           row.Notes,
		CreatedAt:       row.CreatedAt,
	}
}

// CreateDocumentReview records a review of content (documentType's
// content as of right now -- the caller is responsible for reading it
// off disk before calling this, store has no filesystem access). cycle
// is computed here -- the count of existing reviews for this
// application+documentType, plus one -- rather than supplied by the
// caller, so it can't drift out of sequence.
func (s *Store) CreateDocumentReview(ctx context.Context, applicationID int64, documentType, content, outcome, notes string) (DocumentReview, error) {
	count, err := s.queries.CountDocumentReviews(ctx, db.CountDocumentReviewsParams{
		ApplicationID: applicationID,
		DocumentType:  documentType,
	})
	if err != nil {
		return DocumentReview{}, fmt.Errorf("store: count document reviews: %w", err)
	}

	sum := sha256.Sum256([]byte(content))
	row, err := s.queries.CreateDocumentReview(ctx, db.CreateDocumentReviewParams{
		ApplicationID:   applicationID,
		DocumentType:    documentType,
		Cycle:           count + 1,
		ContentSnapshot: content,
		ContentSha256:   hex.EncodeToString(sum[:]),
		Outcome:         outcome,
		Notes:           notes,
	})
	if err != nil {
		return DocumentReview{}, fmt.Errorf("store: create document review: %w", err)
	}
	return documentReviewFromRow(row), nil
}

// LatestDocumentReview returns applicationID's most recent review of
// documentType, if any. ok is false when no review has been recorded yet
// (the common case until the user runs a review) rather than an error.
func (s *Store) LatestDocumentReview(ctx context.Context, applicationID int64, documentType string) (review DocumentReview, ok bool, err error) {
	reviews, err := s.ListDocumentReviews(ctx, applicationID, documentType)
	if err != nil {
		return DocumentReview{}, false, err
	}
	if len(reviews) == 0 {
		return DocumentReview{}, false, nil
	}
	return reviews[0], true, nil
}

// LatestDocumentReviews resolves applicationID's latest review of each
// document type (cover letter, resume) into a map, omitting any document
// type with no review yet -- built on LatestDocumentReview so both stay
// in step with its single-review semantics. Used by
// ListActiveApplications (see application_view.go) and by the TUI
// wherever a per-document-type review summary is needed for one
// application (see decisions.log #83).
func (s *Store) LatestDocumentReviews(ctx context.Context, applicationID int64) (map[string]DocumentReview, error) {
	reviews := make(map[string]DocumentReview)
	for _, documentType := range []string{DocumentTypeCoverLetter, DocumentTypeResume} {
		review, ok, err := s.LatestDocumentReview(ctx, applicationID, documentType)
		if err != nil {
			return nil, err
		}
		if ok {
			reviews[documentType] = review
		}
	}
	return reviews, nil
}

// ListDocumentReviews returns applicationID's reviews for documentType,
// most recent cycle first.
func (s *Store) ListDocumentReviews(ctx context.Context, applicationID int64, documentType string) ([]DocumentReview, error) {
	rows, err := s.queries.ListDocumentReviews(ctx, db.ListDocumentReviewsParams{
		ApplicationID: applicationID,
		DocumentType:  documentType,
	})
	if err != nil {
		return nil, err
	}
	reviews := make([]DocumentReview, len(rows))
	for i, row := range rows {
		reviews[i] = documentReviewFromRow(row)
	}
	return reviews, nil
}
