package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// DocumentType is a typed enum for DocumentReview.DocumentType. Go is the
// sole source of truth for which values are legal, following
// ApplicationStatus's pattern (see application_status.go, decisions.log)
// rather than a DB CHECK constraint -- the document_reviews.document_type
// column dropped its CHECK in migration 00008, once this stopped being
// "a small, stable set with no history of churn" (00007's original
// reasoning) worth a second source of truth. ParseDocumentType is where
// enforcement now happens, at the point a raw DB row is turned into a
// store.DocumentReview.
type DocumentType int

const (
	DocumentTypeCoverLetter DocumentType = iota
	DocumentTypeResume
)

// documentTypeNames holds the DB string form for each DocumentType,
// indexed by its int value -- the single place the Go<->DB string
// mapping is defined; String and ParseDocumentType both go through it so
// they can't drift from each other.
var documentTypeNames = [...]string{
	DocumentTypeCoverLetter: "cover_letter",
	DocumentTypeResume:      "resume",
}

// String implements fmt.Stringer, and is also the value persisted to the
// document_reviews.document_type DB column.
func (d DocumentType) String() string {
	if d < 0 || int(d) >= len(documentTypeNames) {
		return fmt.Sprintf("DocumentType(%d)", int(d))
	}
	return documentTypeNames[d]
}

// MarshalJSON encodes as the same DB string form String() returns (e.g.
// "cover_letter"), not the underlying int -- preemptive, like
// ApplicationStatus's own MarshalJSON: nothing serializes a
// DocumentReview to JSON yet, but a bare int would otherwise be a
// footgun the moment something does (a JSON consumer, or any persisted
// blob, would have to know Swamp's internal enum ordering, which is
// exactly what reordering the const block would silently break).
func (d DocumentType) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// MarshalText implements encoding.TextMarshaler -- the interface
// encoding/json actually consults for map keys. MarshalJSON above is
// NOT consulted there, so a map[DocumentType]X would otherwise silently
// serialize its keys as "0"/"1" (the underlying int) instead of
// "cover_letter"/"resume", even with MarshalJSON already correct for
// every other position (see stage.Candidate/Prepared's LatestReviews
// field, decisions.log).
func (d DocumentType) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextMarshaler's decode half, for
// symmetry -- nothing in this codebase currently decodes a DocumentType
// from JSON, but half-implementing the interface would be a surprise
// waiting to happen.
func (d *DocumentType) UnmarshalText(text []byte) error {
	parsed, err := ParseDocumentType(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// ParseDocumentType converts a raw DB document_type string into the typed
// enum, failing loudly (rather than silently defaulting) if the value
// isn't one of the known types -- since the DB no longer enforces this
// with a CHECK constraint, this is the only place it's still enforced.
func ParseDocumentType(s string) (DocumentType, error) {
	for i, name := range documentTypeNames {
		if name == s {
			return DocumentType(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown document type %q", s)
}

// ReviewOutcome is a typed enum for DocumentReview.Outcome -- same
// reasoning and pattern as DocumentType above.
type ReviewOutcome int

const (
	ReviewOutcomePassed ReviewOutcome = iota
	ReviewOutcomeFlagged
)

// reviewOutcomeNames holds the DB string form for each ReviewOutcome,
// indexed by its int value -- see documentTypeNames above for why.
var reviewOutcomeNames = [...]string{
	ReviewOutcomePassed:  "passed",
	ReviewOutcomeFlagged: "flagged",
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
// "passed"), not the underlying int -- see DocumentType.MarshalJSON
// above for why this is added preemptively.
func (o ReviewOutcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// ParseReviewOutcome converts a raw DB outcome string into the typed
// enum, failing loudly if the value isn't one of the known outcomes --
// see ParseDocumentType above for why.
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
type DocumentReview struct {
	ID              int64
	ApplicationID   int64
	DocumentType    DocumentType
	Cycle           int64
	ContentSnapshot string
	ContentSHA256   string
	Outcome         ReviewOutcome
	Notes           string
	CreatedAt       time.Time
}

// documentReviewFromRow converts a raw sqlc row into a DocumentReview,
// parsing the DB's document_type/outcome columns into their typed enums
// (see ParseDocumentType/ParseReviewOutcome above) -- the DB no longer
// enforces either with a CHECK constraint (migration 00008), so this is
// where an unknown value is caught, the same way applicationFromRow does
// for status.
func documentReviewFromRow(row db.DocumentReview) (DocumentReview, error) {
	documentType, err := ParseDocumentType(row.DocumentType)
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
func (s *Store) CreateDocumentReview(ctx context.Context, applicationID int64, documentType DocumentType, content string, outcome ReviewOutcome, notes string) (DocumentReview, error) {
	count, err := s.queries.CountDocumentReviews(ctx, db.CountDocumentReviewsParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
	})
	if err != nil {
		return DocumentReview{}, fmt.Errorf("store: count document reviews: %w", err)
	}

	sum := sha256.Sum256([]byte(content))
	row, err := s.queries.CreateDocumentReview(ctx, db.CreateDocumentReviewParams{
		ApplicationID:   applicationID,
		DocumentType:    documentType.String(),
		Cycle:           count + 1,
		ContentSnapshot: content,
		ContentSha256:   hex.EncodeToString(sum[:]),
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
func (s *Store) LatestDocumentReview(ctx context.Context, applicationID int64, documentType DocumentType) (review DocumentReview, ok bool, err error) {
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
func (s *Store) LatestDocumentReviews(ctx context.Context, applicationID int64) (map[DocumentType]DocumentReview, error) {
	reviews := make(map[DocumentType]DocumentReview)
	for _, documentType := range []DocumentType{DocumentTypeCoverLetter, DocumentTypeResume} {
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
func (s *Store) ListDocumentReviews(ctx context.Context, applicationID int64, documentType DocumentType) ([]DocumentReview, error) {
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
