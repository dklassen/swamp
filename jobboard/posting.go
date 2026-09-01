// Package jobboard defines the canonical, source-agnostic shape a job
// board client normalizes its API response into. It exists purely as a
// shared vocabulary type: ashby.Posting, greenhouse.Posting, lever.Posting,
// and sync.Posting are all type aliases to jobboard.Posting rather than
// four independently-declared structs that happen to match, so a client
// package's Posting IS sync's Posting -- no field-by-field translation
// code exists between them, and none can silently drop a field.
//
// A field a source's API can't supply (e.g. Greenhouse has no Team,
// EmploymentType, or WorkplaceType) is simply left at its zero value --
// callers never special-case which source produced a Posting (see
// decisions.log, #57).
//
// This package depends on nothing but the standard library, and knows
// nothing about sync or store, so ashby/greenhouse/lever taking on this
// one import doesn't create any new coupling to sync or store -- it's an
// Internal dependency (a plain data type), the same category as
// depending on "time" itself.
package jobboard

import "time"

type Posting struct {
	SourceID        string
	Title           string
	Department      string
	Team            string
	Location        string
	EmploymentType  string
	WorkplaceType   string
	DescriptionHTML string
	DescriptionText string
	JobURL          string
	ApplicationURL  string
	PublishedAt     time.Time
	RawPayload      []byte
}
