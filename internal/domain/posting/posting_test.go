package posting_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/dklassen/swamp/internal/domain/posting"
)

// An unset publish date encodes as null, not as the zero time.
func TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(posting.Posting{ID: 1})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":null`)) {
		t.Errorf("Posting with no publish date encoded as %s, want \"PublishedAt\":null", encoded)
	}
}

func TestPosting_MarshalJSON_RealPublishedAtIsPreserved(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 8, 24, 13, 15, 0, 0, time.UTC)
	encoded, err := json.Marshal(posting.Posting{ID: 1, IngestedFields: posting.IngestedFields{PublishedAt: posting.OptionalTime{Time: published}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":"2026-08-24T13:15:00Z"`)) {
		t.Errorf("Posting encoded as %s, want the real publish date preserved", encoded)
	}
}

// A MarshalJSON on the embedded IngestedFields would be promoted to
// Posting and silently drop every other field from its JSON.
func TestPosting_MarshalJSON_KeepsEveryField(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(posting.Posting{ID: 7, IngestedFields: posting.IngestedFields{Title: "Engineer"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"ID", "CompanyID", "Source", "SourceID", "Title", "Department", "Team",
		"Location", "EmploymentType", "WorkplaceType", "DescriptionHTML",
		"DescriptionText", "JobURL", "ApplicationURL", "PublishedAt",
		"RawPayload", "ListingStatus", "FirstSeenAt", "LastSeenAt",
		"CreatedAt", "UpdatedAt",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("encoded Posting is missing key %q; got %s", key, encoded)
		}
	}
}
