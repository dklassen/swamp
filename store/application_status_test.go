package store

import (
	"encoding/json"
	"testing"
)

func TestApplicationStatus_String_ReturnsDBValue(t *testing.T) {
	if got := ApplicationStatusStarted.String(); got != "application_started" {
		t.Fatalf("ApplicationStatusStarted.String() = %q, want %q", got, "application_started")
	}
}

func TestApplicationStatuses_EachRoundTripsThroughStringAndParse(t *testing.T) {
	for _, status := range ApplicationStatuses() {
		parsed, err := ParseApplicationStatus(status.String())
		if err != nil {
			t.Fatalf("ParseApplicationStatus(%s.String()) error = %v, want nil", status, err)
		}
		if parsed != status {
			t.Fatalf("ParseApplicationStatus(%q) = %v, want %v", status.String(), parsed, status)
		}
	}
}

func TestApplicationStatus_MarshalJSON_UsesDBStringNotIntValue(t *testing.T) {
	got, err := json.Marshal(ApplicationStatusInterviewing)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `"interviewing"`
	if string(got) != want {
		t.Fatalf("json.Marshal(ApplicationStatusInterviewing) = %s, want %s (a bare int forces JSON consumers to know Swamp's internal enum ordering)", got, want)
	}
}

func TestParseApplicationStatus_UnknownValue_ReturnsError(t *testing.T) {
	// "applied" was removed from the valid set by the 00002 migration
	// (see db/migrations/00002_split_application_from_posting.sql) --
	// still a good example of a value that must not parse.
	if _, err := ParseApplicationStatus("applied"); err == nil {
		t.Fatal("ParseApplicationStatus(\"applied\") = nil error, want error (not a valid status)")
	}
}

// TestTerminalApplicationStatuses_AreTheDeadEndStatuses pins the exact
// set, in order. Deliberately not named after its members: the set has
// grown twice (posting_closed, withdrawn) and a name listing them has to
// be rewritten every time, which obscures that the test itself never
// changed.
func TestTerminalApplicationStatuses_AreTheDeadEndStatuses(t *testing.T) {
	got := TerminalApplicationStatuses()
	want := []ApplicationStatus{
		ApplicationStatusRejected,
		ApplicationStatusOfferDeclined,
		ApplicationStatusPostingClosed,
		ApplicationStatusWithdrawn,
	}
	if len(got) != len(want) {
		t.Fatalf("TerminalApplicationStatuses() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TerminalApplicationStatuses() = %v, want %v", got, want)
		}
	}
}

func TestTerminalApplicationStatuses_EveryValueIsAKnownStatus(t *testing.T) {
	known := map[ApplicationStatus]bool{}
	for _, s := range ApplicationStatuses() {
		known[s] = true
	}
	for _, terminal := range TerminalApplicationStatuses() {
		if !known[terminal] {
			t.Fatalf("TerminalApplicationStatuses() contains %v, not in ApplicationStatuses()", terminal)
		}
	}
}

// TestApplicationStatusPostingClosed_IsTerminal covers the status added
// for #105: an application ended because its posting was taken down, as
// distinct from being rejected (nobody rejected anything) or declining
// an offer. Being terminal is what drops it out of
// ListActiveApplications.
func TestApplicationStatusPostingClosed_IsTerminal(t *testing.T) {
	if got := ApplicationStatusPostingClosed.String(); got != "posting_closed" {
		t.Errorf("ApplicationStatusPostingClosed.String() = %q, want %q", got, "posting_closed")
	}

	var found bool
	for _, status := range TerminalApplicationStatuses() {
		if status == ApplicationStatusPostingClosed {
			found = true
		}
	}
	if !found {
		t.Errorf("TerminalApplicationStatuses() = %v, want it to include posting_closed -- a closed posting's application is a dead end and must drop out of the active list", TerminalApplicationStatuses())
	}
}

// TestApplicationStatusWithdrawn_IsTerminal covers the manual
// counterpart to posting_closed: the user changed their mind and is no
// longer applying. Distinct from rejected (nobody rejected them) and
// from offer_declined (there was no offer), both of which would put a
// false fact in a record the user reads back later.
func TestApplicationStatusWithdrawn_IsTerminal(t *testing.T) {
	if got := ApplicationStatusWithdrawn.String(); got != "withdrawn" {
		t.Errorf("ApplicationStatusWithdrawn.String() = %q, want %q", got, "withdrawn")
	}

	var found bool
	for _, status := range TerminalApplicationStatuses() {
		if status == ApplicationStatusWithdrawn {
			found = true
		}
	}
	if !found {
		t.Errorf("TerminalApplicationStatuses() = %v, want it to include withdrawn -- withdrawing is a dead end and must drop out of the active list", TerminalApplicationStatuses())
	}
}
