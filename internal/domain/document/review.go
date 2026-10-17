package document

import (
	"encoding/json"
	"fmt"
	"time"
)

// Outcome is a typed enum for Review.Outcome -- same reasoning and
// pattern as Type.
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
// ParseType for why.
func ParseOutcome(s string) (Outcome, error) {
	for i, name := range outcomeNames {
		if name == s {
			return Outcome(i), nil
		}
	}
	return 0, fmt.Errorf("document: unknown review outcome %q", s)
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
	DocumentType    Type
	Cycle           int64
	ContentSnapshot string
	ContentSHA256   string
	Outcome         Outcome
	Notes           string
	CreatedAt       time.Time
}

// IsCurrent reports whether r still describes content -- i.e. whether the
// document hasn't changed since r was recorded, hashed the same way a
// review hashes content at review time. A review whose content has since
// diverged describes a version of the document that no longer exists;
// callers that surface "the current review status" of a document should
// treat a non-current review the same as no review at all.
func (r Review) IsCurrent(content string) bool {
	return ContentSHA256(content) == r.ContentSHA256
}
