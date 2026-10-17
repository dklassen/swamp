package posting_test

import (
	"encoding/json"
	"testing"

	"github.com/dklassen/swamp/internal/domain/posting"
)

func TestListingStatus_RoundTripsThroughItsDBString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		status posting.ListingStatus
		db     string
	}{
		{posting.ListingOpen, "open"},
		{posting.ListingClosed, "closed"},
	} {
		t.Run(tt.db, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.db {
				t.Errorf("String() = %q, want %q", got, tt.db)
			}
			parsed, err := posting.ParseListingStatus(tt.db)
			if err != nil {
				t.Fatalf("ParseListingStatus(%q): %v", tt.db, err)
			}
			if parsed != tt.status {
				t.Errorf("ParseListingStatus(%q) = %v, want %v", tt.db, parsed, tt.status)
			}
		})
	}
}

// Snapshots and the agent's JSON carry the DB string, not the enum's int.
func TestListingStatus_JSONIsItsDBString(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(posting.ListingClosed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `"` + posting.ListingClosed.String() + `"`; string(encoded) != want {
		t.Errorf("Marshal(ListingClosed) = %s, want %s", encoded, want)
	}
	var decoded posting.ListingStatus
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", encoded, err)
	}
	if decoded != posting.ListingClosed {
		t.Errorf("Unmarshal(%s) = %v, want %v", encoded, decoded, posting.ListingClosed)
	}
	if err := json.Unmarshal([]byte(`"gone"`), &decoded); err == nil {
		t.Error(`Unmarshal("gone") succeeded, want an error`)
	}
}

func TestParseListingStatus_UnknownValue_Fails(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "any", "Open"} {
		if got, err := posting.ParseListingStatus(value); err == nil {
			t.Errorf("ParseListingStatus(%q) = %v, want an error", value, got)
		}
	}
}
