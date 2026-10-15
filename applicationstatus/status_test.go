package applicationstatus

import (
	"encoding/json"
	"testing"
)

func TestStatus_String_ReturnsDBValue(t *testing.T) {
	if got := Started.String(); got != "application_started" {
		t.Fatalf("Started.String() = %q, want %q", got, "application_started")
	}
}

func TestAll_EachRoundTripsThroughStringAndParse(t *testing.T) {
	for _, status := range All() {
		parsed, err := Parse(status.String())
		if err != nil {
			t.Fatalf("Parse(%s.String()) error = %v, want nil", status, err)
		}
		if parsed != status {
			t.Fatalf("Parse(%q) = %v, want %v", status.String(), parsed, status)
		}
	}
}

func TestStatus_MarshalJSON_UsesDBStringNotIntValue(t *testing.T) {
	got, err := json.Marshal(Interviewing)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `"interviewing"`
	if string(got) != want {
		t.Fatalf("json.Marshal(Interviewing) = %s, want %s (a bare int forces JSON consumers to know Swamp's internal enum ordering)", got, want)
	}
}

func TestParse_UnknownValue_ReturnsError(t *testing.T) {
	// "applied" was removed from the valid set by the 00002 migration
	// (see db/migrations/00002_split_application_from_posting.sql) --
	// still a good example of a value that must not parse.
	if _, err := Parse("applied"); err == nil {
		t.Fatal("Parse(\"applied\") = nil error, want error (not a valid status)")
	}
}

// TestTerminal_AreTheDeadEndStatuses pins the exact
// set, in order. Deliberately not named after its members: the set has
// grown twice (posting_closed, withdrawn) and a name listing them has to
// be rewritten every time, which obscures that the test itself never
// changed.
func TestTerminal_AreTheDeadEndStatuses(t *testing.T) {
	got := Terminal()
	want := []Status{
		Rejected,
		OfferDeclined,
		PostingClosed,
		Withdrawn,
	}
	if len(got) != len(want) {
		t.Fatalf("Terminal() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Terminal() = %v, want %v", got, want)
		}
	}
}

func TestTerminal_EveryValueIsAKnownStatus(t *testing.T) {
	known := map[Status]bool{}
	for _, s := range All() {
		known[s] = true
	}
	for _, terminal := range Terminal() {
		if !known[terminal] {
			t.Fatalf("Terminal() contains %v, not in All()", terminal)
		}
	}
}

// TestPostingClosed_IsTerminal covers an application ended because its
// posting was taken down, as distinct from being rejected (nobody rejected anything) or declining
// an offer. Being terminal is what drops it out of
// pursuit.List.
func TestPostingClosed_IsTerminal(t *testing.T) {
	if got := PostingClosed.String(); got != "posting_closed" {
		t.Errorf("PostingClosed.String() = %q, want %q", got, "posting_closed")
	}

	var found bool
	for _, status := range Terminal() {
		if status == PostingClosed {
			found = true
		}
	}
	if !found {
		t.Errorf("Terminal() = %v, want it to include posting_closed -- a closed posting's application is a dead end and must drop out of the active list", Terminal())
	}
}

// TestWithdrawn_IsTerminal covers the manual
// counterpart to posting_closed: the user changed their mind and is no
// longer applying. Distinct from rejected (nobody rejected them) and
// from offer_declined (there was no offer), both of which would put a
// false fact in a record the user reads back later.
func TestWithdrawn_IsTerminal(t *testing.T) {
	if got := Withdrawn.String(); got != "withdrawn" {
		t.Errorf("Withdrawn.String() = %q, want %q", got, "withdrawn")
	}

	var found bool
	for _, status := range Terminal() {
		if status == Withdrawn {
			found = true
		}
	}
	if !found {
		t.Errorf("Terminal() = %v, want it to include withdrawn -- withdrawing is a dead end and must drop out of the active list", Terminal())
	}
}
