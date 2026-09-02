// Package filter is pure, I/O-free matching logic: given a company's
// configured filter rules and a job posting, decide whether the posting
// matches. It has no dependency on jobboard or store — see Posting's doc
// comment for why.
package filter

import (
	"fmt"
	"strings"
)

// Posting is the minimal set of fields filter rules are evaluated against.
// It deliberately does not reuse jobboard.Posting: filter should stay
// decoupled from any one job board's client, since store.Company/postings
// (once the sync package exists) may become the actual match target, and
// jobboard.Posting carries many fields (descriptions, URLs, raw payload)
// filter has no business depending on. Callers convert whatever posting
// shape they have into a filter.Posting at the call site.
type Posting struct {
	Department string
	Location   string
}

// Filter is one configured filter rule, matching a row in the
// company_filters table: a field name (e.g. "department") and a single
// value to match against it.
type Filter struct {
	Field string
	Value string
}

// Match reports whether p satisfies filters. Filters are grouped by Field:
// multiple Filters sharing a Field are OR'd together, and each distinct
// Field present in filters must have at least one matching value (Fields
// are AND'd together). A Field with no configured Filters imposes no
// constraint. filters == nil (or empty) means no constraints at all, so
// every posting matches.
//
// Value comparison is an exact match but case-insensitive: job board data
// casing is inconsistent in practice (e.g. Ashby departments have shown up
// as both "Engineering" and "engineering" across postings), and a filter
// configured by a human typing "Engineering" shouldn't silently miss
// postings that differ only in case. This is not substring/fuzzy matching —
// values must match in full, just not in case.
//
// Match returns an error if filters references a Field not in
// fieldAccessors, rather than silently ignoring it or treating it as
// always-unmatched. company_filters enforces valid field names with a CHECK
// constraint, but this package has no dependency on store or the DB schema,
// so it can't assume filters were validated upstream — fail loudly instead
// of masking a caller bug (e.g. a typo'd field, or a new field added to the
// schema before this package's fieldAccessors was updated to match).
func Match(p Posting, filters []Filter) (bool, error) {
	byField := make(map[string][]string)
	for _, f := range filters {
		byField[f.Field] = append(byField[f.Field], f.Value)
	}

	for field, values := range byField {
		accessor, ok := fieldAccessors[field]
		if !ok {
			return false, fmt.Errorf("filter: unsupported field %q", field)
		}
		postingValue := accessor(p)

		matched := false
		for _, v := range values {
			if strings.EqualFold(postingValue, v) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// fieldAccessors maps a company_filters "field" value to the function that
// reads the corresponding value off a Posting. Adding a new filterable
// field means adding an entry here (and to Posting).
var fieldAccessors = map[string]func(Posting) string{
	"department": func(p Posting) string { return p.Department },
	"location":   func(p Posting) string { return p.Location },
}
