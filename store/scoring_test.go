package store

import "testing"

func TestParseScoringScope(t *testing.T) {
	t.Parallel()

	for _, scope := range ScoringScopes() {
		got, err := ParseScoringScope(scope.String())
		if err != nil || got != scope {
			t.Errorf("ParseScoringScope(%q) = %v, %v; want %v", scope, got, err, scope)
		}
	}
	for _, name := range []string{"unscored", "all"} {
		if _, err := ParseScoringScope(name); err != nil {
			t.Errorf("ParseScoringScope(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "ALL", "rescore"} {
		if _, err := ParseScoringScope(name); err == nil {
			t.Errorf("ParseScoringScope(%q) succeeded, want it refused", name)
		}
	}
}
