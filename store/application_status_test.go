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
