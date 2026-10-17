// Package score is how well postings fit, and which are still waiting for
// a score.
package score

import (
	"fmt"
	"time"
)

// Scores are 1 to 5: coarse enough that a 4 means the same from one
// scoring to the next and one model to another, where 0-100 would ask the
// agent for precision it would invent.
const (
	Min = 1
	Max = 5
)

// PendingReason is why a posting is pending.
type PendingReason int

const (
	// PendingNew: the posting has never been scored.
	PendingNew PendingReason = iota
	// PendingChanged: it was scored, but its content has changed since.
	PendingChanged
	// PendingRescore: it has a score on its current content, but not
	// against the profile a rescore is for.
	PendingRescore
)

// pendingReasonNames is the single name table for PendingReason, indexed
// by its value.
var pendingReasonNames = [...]string{
	PendingNew:     "new",
	PendingChanged: "changed",
	PendingRescore: "rescore",
}

func (r PendingReason) String() string {
	if r < 0 || int(r) >= len(pendingReasonNames) {
		return fmt.Sprintf("PendingReason(%d)", int(r))
	}
	return pendingReasonNames[r]
}

// ParsePendingReason is the reason named s, or an error for any other name.
func ParsePendingReason(s string) (PendingReason, error) {
	for i, name := range pendingReasonNames {
		if name == s {
			return PendingReason(i), nil
		}
	}
	return 0, fmt.Errorf("scoring: unknown pending reason %q", s)
}

// MarshalText encodes r as its name, so JSON readers never see the const
// order.
func (r PendingReason) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText is MarshalText's inverse.
func (r *PendingReason) UnmarshalText(text []byte) error {
	parsed, err := ParsePendingReason(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// PendingPosting is a posting with no score yet on what it says now:
// enough to judge it from, and the content hash its score must name.
type PendingPosting struct {
	ID            int64
	Title         string
	CompanyName   string
	Department    string
	Location      string
	WorkplaceType string
	// PublishedAt is zero when the board didn't say.
	PublishedAt   time.Time
	ContentSHA256 string
	Reason        PendingReason
}

// PendingPostingsPage is one page of pending postings.
type PendingPostingsPage struct {
	Postings []PendingPosting
	// HasMore is whether pending postings follow this page.
	HasMore bool
	// Total is how many are pending in all, when the page was read; 0 on
	// an empty page.
	Total int
}

// Provenance is what produced a batch of scores. The client's name
// and version may be empty: MCP client info is optional.
type Provenance struct {
	Model         string
	ClientName    string
	ClientVersion string
	ProfileSHA256 string
}

// Entry is one posting's score, as the agent gives it.
type Entry struct {
	PostingID int64
	// Score is 1 to 5.
	Score int
	// Reason is the one line shown with the score.
	Reason string
	// ContentSHA256 is the posting's content hash the agent read, which
	// must still be current.
	ContentSHA256 string
}

// Refusal is why an entry wasn't recorded.
type Refusal int

const (
	// NoSuchPosting: no posting has the entry's ID.
	NoSuchPosting Refusal = iota
	// OutOfRange: the score isn't between 1 and 5.
	OutOfRange
	// NoReason: the reason is empty.
	NoReason
	// ContentChanged: the posting's content changed since the agent
	// read it; it stays pending.
	ContentChanged
	// Repeat: the posting already has a score on this content,
	// against this profile, from this model.
	Repeat
)

// refusalNames is the single name table for Refusal, indexed by
// its value.
var refusalNames = [...]string{
	NoSuchPosting:  "no_such_posting",
	OutOfRange:     "score_out_of_range",
	NoReason:       "no_reason",
	ContentChanged: "content_changed",
	Repeat:         "already_scored",
}

func (r Refusal) String() string {
	if r < 0 || int(r) >= len(refusalNames) {
		return fmt.Sprintf("Refusal(%d)", int(r))
	}
	return refusalNames[r]
}

// Refusals returns every refusal, in const order.
func Refusals() []Refusal {
	refusals := make([]Refusal, len(refusalNames))
	for i := range refusalNames {
		refusals[i] = Refusal(i)
	}
	return refusals
}

// ParseRefusal is the refusal named s, or an error for any other name.
func ParseRefusal(s string) (Refusal, error) {
	for i, name := range refusalNames {
		if name == s {
			return Refusal(i), nil
		}
	}
	return 0, fmt.Errorf("scoring: unknown score refusal %q", s)
}

// MarshalText encodes r as its name, so JSON readers never see the const
// order.
func (r Refusal) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText is MarshalText's inverse.
func (r *Refusal) UnmarshalText(text []byte) error {
	parsed, err := ParseRefusal(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// Refused is an entry that wasn't recorded, and why.
type Refused struct {
	PostingID int64
	Why       Refusal
}

// RecordResult is what recording did with each entry.
type RecordResult struct {
	// Recorded is the posting IDs scored, in the entries' order.
	Recorded []int64
	// Refused is the entries not recorded, in the entries' order.
	Refused []Refused
}

// Score is one score a posting was given, and what produced it.
type Score struct {
	ID int64
	Provenance
	Score         int
	Reason        string
	ContentSHA256 string
	ScoredAt      time.Time
}
