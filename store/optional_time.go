package store

import "time"

// OptionalTime is a time whose zero value means "not known" and encodes
// as JSON null rather than "0001-01-01T00:00:00Z" (see issue #80).
//
// This exists as a type rather than as a MarshalJSON on the struct that
// holds the field, because the struct-level version is a trap. The field
// it's used for, IngestedFields.PublishedAt, lives in a struct that
// Posting embeds anonymously and deliberately leaves untagged so its
// fields stay promoted into Posting's own JSON object (see #59). A
// MarshalJSON defined on IngestedFields would be promoted along with
// them and would hijack the whole of Posting's encoding, silently
// emitting only IngestedFields' fields and dropping ID, CompanyID,
// ListingStatus and every timestamp from the agent contract, with no
// error anywhere. Defining it on Posting instead avoids that but needs a
// local alias type to dodge infinite recursion plus a shadowing field at
// the right depth, which is hand-maintained cleverness that the next
// person has to re-derive.
//
// Putting the encoding on the value's own type removes both problems.
// The optionality lives with the thing that is optional, nothing
// encloses anything, and the worst a future edit can do is change how
// one scalar renders rather than silently drop fields from a contract.
//
// time.Time is embedded rather than aliased so the usual methods
// (IsZero, Equal, Format, Before/After) keep working on the field
// directly.
type OptionalTime struct {
	time.Time
}

// MarshalJSON encodes the zero value as null and anything else exactly
// as time.Time would. Deleting this method degrades to the embedded
// time.Time's own MarshalJSON, i.e. back to the sentinel string --
// noisy, but not destructive, and
// TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull catches it.
func (t OptionalTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return t.Time.MarshalJSON()
}
