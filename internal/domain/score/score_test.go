package score_test

import (
	"encoding/json"
	"testing"

	"github.com/dklassen/swamp/internal/domain/score"
)

// The vocabularies round-trip through JSON, and an unknown name is
// refused rather than read as the first value.
func TestVocabularies_RoundTripThroughJSON_AndRefuseUnknownNames(t *testing.T) {
	t.Parallel()

	for _, r := range []score.PendingReason{score.PendingNew, score.PendingChanged, score.PendingRescore} {
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", r, err)
		}
		var decoded score.PendingReason
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != r {
			t.Errorf("%v: encoded %s, decoded %v (%v)", r, encoded, decoded, err)
		}
	}
	for _, r := range score.Refusals() {
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", r, err)
		}
		var decoded score.Refusal
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != r {
			t.Errorf("%v: encoded %s, decoded %v (%v)", r, encoded, decoded, err)
		}
	}
	var reason score.PendingReason
	if err := json.Unmarshal([]byte(`"stale"`), &reason); err == nil {
		t.Error(`decoding the pending reason "stale" succeeded, want it refused`)
	}
	var refusal score.Refusal
	if err := json.Unmarshal([]byte(`"too_high"`), &refusal); err == nil {
		t.Error(`decoding the refusal "too_high" succeeded, want it refused`)
	}
}
