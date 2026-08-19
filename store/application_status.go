package store

import "fmt"

// ApplicationStatus is a typed enum for Application.Status. Go is the sole
// source of truth for which values are legal (see decisions.log,
// 2026-08-19 "Application status becomes a Go enum") -- the DB column is
// plain TEXT with no CHECK constraint, so nothing below the store package
// boundary can enforce this; ParseApplicationStatus is where that
// enforcement happens, at the point a raw DB row is turned into a
// store.Application.
type ApplicationStatus int

const (
	ApplicationStatusStarted ApplicationStatus = iota
	ApplicationStatusSubmitted
	ApplicationStatusInterviewing
	ApplicationStatusRejected
	ApplicationStatusOfferReceived
	ApplicationStatusOfferAccepted
	ApplicationStatusOfferDeclined
)

// applicationStatusNames holds the DB string form for each
// ApplicationStatus, indexed by its int value -- the single place the
// Go<->DB string mapping is defined; String and ParseApplicationStatus
// both go through it so they can't drift from each other.
var applicationStatusNames = [...]string{
	ApplicationStatusStarted:       "application_started",
	ApplicationStatusSubmitted:     "application_submitted",
	ApplicationStatusInterviewing:  "interviewing",
	ApplicationStatusRejected:      "rejected",
	ApplicationStatusOfferReceived: "offer_received",
	ApplicationStatusOfferAccepted: "offer_accepted",
	ApplicationStatusOfferDeclined: "offer_declined",
}

// String implements fmt.Stringer, and is also the value persisted to the
// applications.status DB column (see UpdateApplicationStatus).
func (s ApplicationStatus) String() string {
	if s < 0 || int(s) >= len(applicationStatusNames) {
		return fmt.Sprintf("ApplicationStatus(%d)", int(s))
	}
	return applicationStatusNames[s]
}

// ParseApplicationStatus converts a raw DB status string into the typed
// enum, failing loudly (rather than silently defaulting) if the value
// isn't one of the known statuses -- since the DB no longer enforces this
// with a CHECK constraint, this is the only place it's still enforced.
func ParseApplicationStatus(s string) (ApplicationStatus, error) {
	for i, name := range applicationStatusNames {
		if name == s {
			return ApplicationStatus(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown application status %q", s)
}

// ApplicationStatuses returns every valid status in canonical order, for
// UI enumeration (e.g. the TUI's status-select screen) -- the schema has
// no transition graph (any status is legal from any other), so callers
// that want "what can this become" get the full list, not a filtered one.
func ApplicationStatuses() []ApplicationStatus {
	out := make([]ApplicationStatus, len(applicationStatusNames))
	for i := range applicationStatusNames {
		out[i] = ApplicationStatus(i)
	}
	return out
}
