// Package docrecord records what happened to an application's documents:
// each write Swamp saw, each review, and each PDF export.
package docrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// Outcome is a typed enum for Review.Outcome -- same reasoning and
// pattern as documents.Type.
type Outcome int

const (
	Passed Outcome = iota
	Flagged
)

// outcomeNames holds the DB string form for each Outcome, indexed by its
// int value -- the single place the Go<->DB string mapping is defined, so
// String and ParseOutcome can't drift.
var outcomeNames = [...]string{
	Passed:  "passed",
	Flagged: "flagged",
}

// Outcomes returns every review outcome, in const order -- so a caller
// mapping each outcome to something (e.g. the TUI's display) can test it
// has them all.
func Outcomes() []Outcome {
	all := make([]Outcome, len(outcomeNames))
	for i := range outcomeNames {
		all[i] = Outcome(i)
	}
	return all
}

// String implements fmt.Stringer, and is also the value persisted to the
// document_reviews.outcome DB column.
func (o Outcome) String() string {
	if o < 0 || int(o) >= len(outcomeNames) {
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
	return outcomeNames[o]
}

// MarshalJSON encodes as the same DB string form String() returns (e.g.
// "passed"), not the underlying int.
func (o Outcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// ParseOutcome converts a raw DB outcome string into the typed enum,
// failing loudly if the value isn't one of the known outcomes -- see
// documents.ParseType for why.
func ParseOutcome(s string) (Outcome, error) {
	for i, name := range outcomeNames {
		if name == s {
			return Outcome(i), nil
		}
	}
	return 0, fmt.Errorf("docrecord: unknown review outcome %q", s)
}

// Review is one human pass over a drafted cover letter or resume, owned
// by the user and append-only -- never edited or deleted once created.
// ContentSnapshot captures the document's content as of the moment it was
// reviewed, since documents.Store overwrites cover_letter.md/resume.md in
// place with no versioning of its own: without a snapshot, a review would
// become unreadable the moment the file gets redrafted. ContentSHA256 is a
// derived shortcut for "did this change since the last review" without
// comparing full ContentSnapshot values.
//
// Whether a document has a review is never a property of a Review: it's
// whether the review map (LatestReviews, then documents.Current) has an
// entry for the document. Check that with a comma-ok lookup. A zero
// Review's Outcome reads as passed, so inferring presence from its fields
// would show an unreviewed document as passed.
type Review struct {
	ID              int64
	ApplicationID   int64
	DocumentType    documents.Type
	Cycle           int64
	ContentSnapshot string
	ContentSHA256   string
	Outcome         Outcome
	Notes           string
	CreatedAt       time.Time
}

// IsCurrent reports whether r still describes content -- i.e. whether the
// document hasn't changed since r was recorded, hashed the same way
// CreateReview hashes content at review time. A review whose content has
// since diverged describes a version of the document that no longer
// exists; callers that surface "the current review status" of a document
// should treat a non-current review the same as no review at all.
func (r Review) IsCurrent(content string) bool {
	return documents.ContentSHA256(content) == r.ContentSHA256
}

// reviewFromRow parses the DB's document_type/outcome columns into their
// typed enums -- the DB doesn't enforce either with a CHECK constraint, so
// this is where an unknown value is caught.
func reviewFromRow(row sqlcdb.DocumentReview) (Review, error) {
	documentType, err := documents.ParseType(row.DocumentType)
	if err != nil {
		return Review{}, err
	}
	outcome, err := ParseOutcome(row.Outcome)
	if err != nil {
		return Review{}, err
	}
	return Review{
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

// CreateReview records a review of content (documentType's content as of
// right now -- the caller reads it off disk). Its cycle is the count of
// existing reviews for this application and document type, plus one. The
// count and the insert run in one savepoint, so two reviews can't both
// read the same count and record the same cycle.
func CreateReview(ctx context.Context, tx store.Tx, applicationID int64, documentType documents.Type, content string, outcome Outcome, notes string) (Review, error) {
	var row sqlcdb.DocumentReview
	err := store.InTx(ctx, tx, func(tx store.Tx) error {
		var err error
		row, err = store.Read(ctx, tx, func(q *sqlcdb.Queries) (sqlcdb.DocumentReview, error) {
			count, err := q.CountDocumentReviews(ctx, sqlcdb.CountDocumentReviewsParams{
				ApplicationID: applicationID,
				DocumentType:  documentType.String(),
			})
			if err != nil {
				return sqlcdb.DocumentReview{}, fmt.Errorf("count document reviews: %w", err)
			}
			return q.CreateDocumentReview(ctx, sqlcdb.CreateDocumentReviewParams{
				ApplicationID:   applicationID,
				DocumentType:    documentType.String(),
				Cycle:           count + 1,
				ContentSnapshot: content,
				ContentSha256:   documents.ContentSHA256(content),
				Outcome:         outcome.String(),
				Notes:           notes,
			})
		})
		return err
	})
	if err != nil {
		return Review{}, fmt.Errorf("docrecord: create review: %w", err)
	}
	return reviewFromRow(row)
}

// LatestReview returns applicationID's most recent review of
// documentType, if any. ok is false when no review has been recorded yet
// (the common case until the user runs a review) rather than an error.
func LatestReview(ctx context.Context, r store.Runner, applicationID int64, documentType documents.Type) (review Review, ok bool, err error) {
	reviews, err := ListReviews(ctx, r, applicationID, documentType)
	if err != nil {
		return Review{}, false, err
	}
	if len(reviews) == 0 {
		return Review{}, false, nil
	}
	return reviews[0], true, nil
}

// LatestReviews resolves applicationID's latest review of each document
// type into a map, omitting any document type with no review yet. A
// missing entry is the only "no review" signal; see Review.
func LatestReviews(ctx context.Context, r store.Runner, applicationID int64) (map[documents.Type]Review, error) {
	reviews := make(map[documents.Type]Review)
	for _, documentType := range documents.Types() {
		review, ok, err := LatestReview(ctx, r, applicationID, documentType)
		if err != nil {
			return nil, err
		}
		if ok {
			reviews[documentType] = review
		}
	}
	return reviews, nil
}

// ListReviews returns applicationID's reviews for documentType, most
// recent cycle first.
func ListReviews(ctx context.Context, r store.Runner, applicationID int64, documentType documents.Type) ([]Review, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.DocumentReview, error) {
		return q.ListDocumentReviews(ctx, sqlcdb.ListDocumentReviewsParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("docrecord: list reviews: %w", err)
	}
	reviews := make([]Review, len(rows))
	for i, row := range rows {
		review, err := reviewFromRow(row)
		if err != nil {
			return nil, err
		}
		reviews[i] = review
	}
	return reviews, nil
}
