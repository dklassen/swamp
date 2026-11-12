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
	encoded, err := json.Marshal(posting.Posting{ID: 1, PublishedAt: posting.OptionalTime{Time: published}})
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

	encoded, err := json.Marshal(posting.Posting{ID: 7, Title: "Engineer"})
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

func TestContentSHA256_EveryFieldChangesTheHash(t *testing.T) {
	t.Parallel()

	base := posting.IngestedFields{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}
	tests := []struct {
		name    string
		changed posting.IngestedFields
	}{
		{"title", posting.IngestedFields{Title: "Engineer II", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"department", posting.IngestedFields{Title: "Engineer", Department: "Data", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"location", posting.IngestedFields{Title: "Engineer", Department: "Platform", Location: "Cork", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
		{"workplace type", posting.IngestedFields{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Remote", DescriptionText: "Build things."}},
		{"description", posting.IngestedFields{Title: "Engineer", Department: "Platform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build other things."}},
		// The same text split differently between fields is other content.
		{"a field boundary", posting.IngestedFields{Title: "EngineerP", Department: "latform", Location: "Dublin", WorkplaceType: "Hybrid", DescriptionText: "Build things."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if posting.ContentSHA256(base) == posting.ContentSHA256(tt.changed) {
				t.Errorf("changing the %s left the hash at %s", tt.name, posting.ContentSHA256(base))
			}
		})
	}
	if same := base; posting.ContentSHA256(same) != posting.ContentSHA256(base) {
		t.Error("the same content hashed differently")
	}
	if got := len(posting.ContentSHA256(base)); got != 64 {
		t.Errorf("hash %q is %d characters, want 64 hex", posting.ContentSHA256(base), got)
	}
}
