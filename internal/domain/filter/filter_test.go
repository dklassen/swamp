package filter

import "testing"

// assertMatch calls Match and fails the test if it errors or its result
// doesn't equal want.
func assertMatch(t *testing.T, p Posting, filters []Filter, want bool) {
	t.Helper()
	got, err := Match(p, filters)
	if err != nil {
		t.Fatalf("Match(%+v, %+v): %v", p, filters, err)
	}
	if got != want {
		t.Fatalf("Match(%+v, %+v) = %v, want %v", p, filters, got, want)
	}
}

func TestMatch_NoFiltersConfigured_AlwaysMatches(t *testing.T) {
	p := Posting{Department: "Engineering", Location: "Remote"}

	assertMatch(t, p, nil, true)
}

func TestMatch_SingleDepartmentFilter_MatchingDepartment_Matches(t *testing.T) {
	p := Posting{Department: "Engineering", Location: "Remote"}
	filters := []Filter{{Field: "department", Value: "Engineering"}}

	assertMatch(t, p, filters, true)
}

func TestMatch_SingleDepartmentFilter_DifferentDepartment_Excluded(t *testing.T) {
	p := Posting{Department: "Sales", Location: "Remote"}
	filters := []Filter{{Field: "department", Value: "Engineering"}}

	assertMatch(t, p, filters, false)
}

func TestMatch_MultipleValuesSameField_MatchesEither(t *testing.T) {
	filters := []Filter{
		{Field: "department", Value: "Engineering"},
		{Field: "department", Value: "Data Engineering"},
	}

	assertMatch(t, Posting{Department: "Engineering"}, filters, true)
	assertMatch(t, Posting{Department: "Data Engineering"}, filters, true)
	assertMatch(t, Posting{Department: "Sales"}, filters, false)
}

func TestMatch_TwoFieldsConfigured_RequiresBothToMatch(t *testing.T) {
	filters := []Filter{
		{Field: "department", Value: "Engineering"},
		{Field: "location", Value: "Remote"},
	}

	assertMatch(t, Posting{Department: "Engineering", Location: "Remote"}, filters, true)
	assertMatch(t, Posting{Department: "Engineering", Location: "New York"}, filters, false)
	assertMatch(t, Posting{Department: "Sales", Location: "Remote"}, filters, false)
	assertMatch(t, Posting{Department: "Sales", Location: "New York"}, filters, false)
}

func TestMatch_ComparisonIsCaseInsensitive(t *testing.T) {
	p := Posting{Department: "engineering"}
	filters := []Filter{{Field: "department", Value: "Engineering"}}

	assertMatch(t, p, filters, true)
}

func TestMatch_UnsupportedField_ReturnsError(t *testing.T) {
	p := Posting{Department: "Engineering"}
	filters := []Filter{{Field: "seniority", Value: "Senior"}}

	_, err := Match(p, filters)
	if err == nil {
		t.Fatalf("Match: expected error for unsupported field %q, got nil", filters[0].Field)
	}
}
