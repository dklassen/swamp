package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store/db"
)

// ReviewOutcome is a typed enum for DocumentReview.Outcome -- same
// reasoning and pattern as documents.Type.
type ReviewOutcome int

const (
	ReviewOutcomePassed ReviewOutcome = iota
	ReviewOutcomeFlagged
)

// reviewOutcomeNames holds the DB string form for each ReviewOutcome,
// indexed by its int value -- the single place the Go<->DB string mapping
// is defined, so String and ParseReviewOutcome can't drift.
var reviewOutcomeNames = [...]string{
	ReviewOutcomePassed:  "passed",
	ReviewOutcomeFlagged: "flagged",
}

// ReviewOutcomes returns every review outcome, in const order -- so a
// caller mapping each outcome to something (e.g. the TUI's display) can
// test it has them all.
func ReviewOutcomes() []ReviewOutcome {
	all := make([]ReviewOutcome, len(reviewOutcomeNames))
	for i := range reviewOutcomeNames {
		all[i] = ReviewOutcome(i)
	}
	return all
}

// String implements fmt.Stringer, and is also the value persisted to the
// document_reviews.outcome DB column.
func (o ReviewOutcome) String() string {
	if o < 0 || int(o) >= len(reviewOutcomeNames) {
		return fmt.Sprintf("ReviewOutcome(%d)", int(o))
	}
	return reviewOutcomeNames[o]
}

// MarshalJSON encodes as the same DB string form String() returns (e.g.
// "passed"), not the underlying int -- see documents.Type.MarshalJSON
// above for why this is added preemptively.
func (o ReviewOutcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// ParseReviewOutcome converts a raw DB outcome string into the typed
// enum, failing loudly if the value isn't one of the known outcomes --
// see documents.ParseType for why.
func ParseReviewOutcome(s string) (ReviewOutcome, error) {
	for i, name := range reviewOutcomeNames {
		if name == s {
			return ReviewOutcome(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown review outcome %q", s)
}

// DocumentReview is one human pass over a drafted cover letter or resume,
// owned by the user and append-only -- never edited or deleted once
// created (see decisions.log, #51). ContentSnapshot captures the
// document's content as of the moment it was reviewed, since
// documents.Store overwrites cover_letter.md/resume.md in place with no
// versioning of its own: without a snapshot, a review would become
// unreadable the moment the file gets redrafted. ContentSHA256 is a
// derived shortcut for "did this change since the last review" without
// comparing full ContentSnapshot values.
//
// Whether a document has a review is never a property of a
// DocumentReview: it's whether the review map (LatestDocumentReviews,
// then documents.Current) has an entry for the document. Check that with
// a comma-ok lookup. A zero DocumentReview's Outcome reads as passed, so
// inferring presence from its fields would show an unreviewed document
// as passed (RFC 0005).
type DocumentReview struct {
	ID              int64
	ApplicationID   int64
	DocumentType    documents.Type
	Cycle           int64
	ContentSnapshot string
	ContentSHA256   string
	Outcome         ReviewOutcome
	Notes           string
	CreatedAt       time.Time
}

// IsCurrent reports whether r still describes content -- i.e. whether
// the document hasn't changed since r was recorded, computed the same
// way CreateDocumentReview hashes content at review time, so the two can
// never disagree about what "matches" means. A review whose content has
// since diverged (whether the document was revised in direct response
// to the review, or edited independently) describes a version of the
// document that no longer exists; callers that surface "the current
// review status" of a document (see decisions.log, stage.List/Prepare
// and the TUI's review badges) should treat a non-current review the
// same as no review at all, not as still describing what's on disk now.
func (r DocumentReview) IsCurrent(content string) bool {
	return contentSHA256(content) == r.ContentSHA256
}

// contentSHA256 is the hex SHA-256 of a document's content: what a review
// or an export records, and what IsCurrent compares against.
func contentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// documentReviewFromRow converts a raw sqlc row into a DocumentReview,
// parsing the DB's document_type/outcome columns into their typed enums
// (see documents.ParseType and ParseReviewOutcome) -- the DB no longer
// enforces either with a CHECK constraint (migration 00008), so this is
// where an unknown value is caught, the same way applicationFromRow does
// for status.
func documentReviewFromRow(row db.DocumentReview) (DocumentReview, error) {
	documentType, err := documents.ParseType(row.DocumentType)
	if err != nil {
		return DocumentReview{}, err
	}
	outcome, err := ParseReviewOutcome(row.Outcome)
	if err != nil {
		return DocumentReview{}, err
	}
	return DocumentReview{
		ID:              row.ID,
		ApplicationID:   row.ApplicationID,
		DocumentType:    documentType,
		Cycle:           row.Cycle,
		ContentSnapshot: row.ContentSnapshot,
		ContentSHA256:   row.ContentSha256,
		Outcome:         outcome,
		Notes:           row.Notes,
		CreatedAt:       row.CreatedAt,
	}, nil
}

// CreateDocumentReview records a review of content (documentType's
// content as of right now -- the caller is responsible for reading it
// off disk before calling this, store has no filesystem access). cycle
// is computed here -- the count of existing reviews for this
// application+documentType, plus one -- rather than supplied by the
// caller, so it can't drift out of sequence.
func (s *Store) CreateDocumentReview(ctx context.Context, applicationID int64, documentType documents.Type, content string, outcome ReviewOutcome, notes string) (DocumentReview, error) {
	count, err := s.queries.CountDocumentReviews(ctx, db.CountDocumentReviewsParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
	})
	if err != nil {
		return DocumentReview{}, fmt.Errorf("store: count document reviews: %w", err)
	}

	row, err := s.queries.CreateDocumentReview(ctx, db.CreateDocumentReviewParams{
		ApplicationID:   applicationID,
		DocumentType:    documentType.String(),
		Cycle:           count + 1,
		ContentSnapshot: content,
		ContentSha256:   contentSHA256(content),
		Outcome:         outcome.String(),
		Notes:           notes,
	})
	if err != nil {
		return DocumentReview{}, fmt.Errorf("store: create document review: %w", err)
	}
	return documentReviewFromRow(row)
}

// LatestDocumentReview returns applicationID's most recent review of
// documentType, if any. ok is false when no review has been recorded yet
// (the common case until the user runs a review) rather than an error.
func (s *Store) LatestDocumentReview(ctx context.Context, applicationID int64, documentType documents.Type) (review DocumentReview, ok bool, err error) {
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
// application (see decisions.log #83). A missing entry is the only "no
// review" signal; see DocumentReview.
func (s *Store) LatestDocumentReviews(ctx context.Context, applicationID int64) (map[documents.Type]DocumentReview, error) {
	reviews := make(map[documents.Type]DocumentReview)
	for _, documentType := range documents.Types() {
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
func (s *Store) ListDocumentReviews(ctx context.Context, applicationID int64, documentType documents.Type) ([]DocumentReview, error) {
	rows, err := s.queries.ListDocumentReviews(ctx, db.ListDocumentReviewsParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
	})
	if err != nil {
		return nil, err
	}
	reviews := make([]DocumentReview, len(rows))
	for i, row := range rows {
		review, err := documentReviewFromRow(row)
		if err != nil {
			return nil, err
		}
		reviews[i] = review
	}
	return reviews, nil
}
