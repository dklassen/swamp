package application

import (
	"encoding/json"
	"fmt"
)

// Status is a typed enum for Application.Status. Go is the sole
// source of truth for which values are legal -- the DB column is plain
// TEXT with no CHECK constraint, so nothing below the store package
// boundary can enforce this; ParseStatus is where that
// enforcement happens, at the point a raw DB row is turned into a
// Application.
type Status int

const (
	Started Status = iota
	Submitted
	Interviewing
	Rejected
	OfferReceived
	OfferAccepted
	OfferDeclined
	PostingClosed
	Withdrawn
)

// statusNames holds the DB string form for each
// Status, indexed by its int value -- the single place the
// Go<->DB string mapping is defined; String and ParseStatus
// both go through it so they can't drift from each other.
var statusNames = [...]string{
	Started:       "application_started",
	Submitted:     "application_submitted",
	Interviewing:  "interviewing",
	Rejected:      "rejected",
	OfferReceived: "offer_received",
	OfferAccepted: "offer_accepted",
	OfferDeclined: "offer_declined",
	PostingClosed: "posting_closed",
	Withdrawn:     "withdrawn",
}

// String implements fmt.Stringer, and is also the value persisted to the
// applications.status DB column (see application.UpdateStatus).
func (s Status) String() string {
	if s < 0 || int(s) >= len(statusNames) {
		return fmt.Sprintf("Status(%d)", int(s))
	}
	return statusNames[s]
}

// MarshalJSON encodes as the same DB string form String() returns (e.g.
// "application_started"), not the underlying int -- callers outside this
// package (e.g. the stage package's CLI JSON output) shouldn't need to
// know this enum's internal ordering to make sense of a status.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// ParseStatus converts a raw DB status string into the typed
// enum, failing loudly (rather than silently defaulting) if the value
// isn't one of the known statuses -- since the DB no longer enforces this
// with a CHECK constraint, this is the only place it's still enforced.
func ParseStatus(s string) (Status, error) {
	for i, name := range statusNames {
		if name == s {
			return Status(i), nil
		}
	}
	return 0, fmt.Errorf("application: unknown status %q", s)
}

// All returns every valid status in canonical order, for
// UI enumeration (e.g. the TUI's status-select screen) -- the schema has
// no transition graph (any status is legal from any other), so callers
// that want "what can this become" get the full list, not a filtered one.
func All() []Status {
	out := make([]Status, len(statusNames))
	for i := range statusNames {
		out[i] = Status(i)
	}
	return out
}

// terminal holds the statuses pursuit.List
// excludes -- dead ends the user isn't still pursuing. This is the sole
// source of truth for "terminal": the query passes this list to sqlc's
// slice macro at call time rather than hardcoding status strings in SQL,
// so there's exactly one place this business rule can be edited.
var terminal = []Status{
	Rejected,
	OfferDeclined,
	PostingClosed,
	Withdrawn,
}

// Terminal returns the statuses that count as a dead
// end for an application -- see terminal.
func Terminal() []Status {
	out := make([]Status, len(terminal))
	copy(out, terminal)
	return out
}
