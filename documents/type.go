package documents

import (
	"encoding/json"
	"fmt"
)

// Type is a kind of application document. It was store.DocumentType until
// RFC 0004 moved it here, so that one table owns everything about a
// document type: its name, its label and (through the name) its file.
// store imports this package to persist a Type; this package imports
// nothing from Swamp.
//
// Go is the only source of truth for which types are legal: the
// document_reviews.document_type column has no CHECK constraint since
// migration 00008. ParseType enforces it when a DB row becomes a review.
type Type int

const (
	CoverLetter Type = iota
	Resume
)

// types is the single list of document types, indexed by Type. Adding a
// type is one entry here plus its constant above; TestTypes_EachHasANameLabelAndDocument
// fails if the entry is incomplete.
//
// name is both the file's base name (<name>.md) and the value stored in
// document_reviews.document_type and document_exports.document_type, so
// renaming one means migrating those columns too.
var types = [...]struct {
	name  string
	label string
}{
	CoverLetter: {name: "cover_letter", label: "cover letter"},
	Resume:      {name: "resume", label: "resume"},
}

// Types returns every document type, in const order.
func Types() []Type {
	all := make([]Type, len(types))
	for i := range types {
		all[i] = Type(i)
	}
	return all
}

func (t Type) valid() bool {
	return t >= 0 && int(t) < len(types)
}

// String implements fmt.Stringer. It's the type's name: the file's base
// name and the value persisted to the database.
func (t Type) String() string {
	if !t.valid() {
		return fmt.Sprintf("Type(%d)", int(t))
	}
	return types[t].name
}

// Label is how the TUI refers to the type in a sentence, e.g. "cover
// letter".
func (t Type) Label() string {
	if !t.valid() {
		return t.String()
	}
	return types[t].label
}

// ParseType converts a stored type name back into a Type, failing for
// any name not in the list rather than defaulting to one.
func ParseType(s string) (Type, error) {
	for i, entry := range types {
		if entry.name == s {
			return Type(i), nil
		}
	}
	return 0, fmt.Errorf("documents: unknown document type %q", s)
}

// MarshalJSON encodes a Type as its name, not the underlying int, so a
// JSON consumer never depends on the const order.
func (t Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// MarshalText implements encoding.TextMarshaler, which encoding/json uses
// for map keys (MarshalJSON isn't consulted there): without it a
// map[Type]X, like stage's LatestReviews, would serialize keys as "0",
// "1".
func (t Type) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

// UnmarshalText is MarshalText's inverse.
func (t *Type) UnmarshalText(text []byte) error {
	parsed, err := ParseType(string(text))
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}
