// Package filter is a company's filter rules: which of its postings its
// list shows.
package filter

// Filter is one configured filter rule, matching a row in the
// company_filters table: a field name (e.g. "department") and a single
// value to match against it.
type Filter struct {
	Field string
	Value string
}

// Field name constants for Filter.Field / company_filters.field -- the
// single source of truth every caller building or reading a Filter should
// use instead of retyping the string, so one caller's string and
// another's can't drift apart.
const (
	FieldDepartment = "department"
	FieldLocation   = "location"
)
