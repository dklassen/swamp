package store

import "testing"

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

func TestParseApplicationStatus_UnknownValue_ReturnsError(t *testing.T) {
	// "applied" was removed from the valid set by the 00002 migration
	// (see db/migrations/00002_split_application_from_posting.sql) --
	// still a good example of a value that must not parse.
	if _, err := ParseApplicationStatus("applied"); err == nil {
		t.Fatal("ParseApplicationStatus(\"applied\") = nil error, want error (not a valid status)")
	}
}
