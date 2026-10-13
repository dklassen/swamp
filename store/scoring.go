package store

import "fmt"

// ScoringScope is which postings a scoring run covers (RFC 0009).
type ScoringScope int

const (
	// ScoringScopeUnscored: postings with no score on their current
	// content, so new and changed ones.
	ScoringScopeUnscored ScoringScope = iota
	// ScoringScopeAll: every eligible posting, for a rescore after a
	// profile change or with another model.
	ScoringScopeAll
)

// scoringScopeNames is the single name table for ScoringScope, indexed by
// its value; scoring_runs.scope holds these, with no CHECK (00028).
var scoringScopeNames = [...]string{
	ScoringScopeUnscored: "unscored",
	ScoringScopeAll:      "all",
}

func (s ScoringScope) String() string {
	if s < 0 || int(s) >= len(scoringScopeNames) {
		return fmt.Sprintf("ScoringScope(%d)", int(s))
	}
	return scoringScopeNames[s]
}

// ParseScoringScope is the scope named s, or an error for any other name.
func ParseScoringScope(s string) (ScoringScope, error) {
	for i, name := range scoringScopeNames {
		if name == s {
			return ScoringScope(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown scoring scope %q: want unscored or all", s)
}

// ScoringScopes returns every scope, in const order.
func ScoringScopes() []ScoringScope {
	scopes := make([]ScoringScope, len(scoringScopeNames))
	for i := range scoringScopeNames {
		scopes[i] = ScoringScope(i)
	}
	return scopes
}
