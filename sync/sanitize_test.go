package sync

import (
	"testing"
	"time"

	"github.com/dklassen/swamp/jobboard"
)

func TestSanitizePosting_TrimsWhitespaceOnStringFields(t *testing.T) {
	published := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := []byte(`{"id":" job-1 "}`)

	p := jobboard.Posting{
		SourceID:        " job-1 ",
		Title:           "  Engineer ",
		Department:      " Engineering",
		Team:            "Core ",
		Location:        " Toronto, Canada ",
		EmploymentType:  " FullTime ",
		WorkplaceType:   " Remote ",
		DescriptionHTML: " <p>desc</p> ",
		DescriptionText: " desc ",
		JobURL:          " https://example.com/job-1 ",
		ApplicationURL:  " https://example.com/job-1/apply ",
		PublishedAt:     published,
		RawPayload:      raw,
	}

	got := sanitizePosting(p)

	want := jobboard.Posting{
		SourceID:        "job-1",
		Title:           "Engineer",
		Department:      "Engineering",
		Team:            "Core",
		Location:        "Toronto, Canada",
		EmploymentType:  "FullTime",
		WorkplaceType:   "Remote",
		DescriptionHTML: "<p>desc</p>",
		DescriptionText: "desc",
		JobURL:          "https://example.com/job-1",
		ApplicationURL:  "https://example.com/job-1/apply",
		PublishedAt:     published,
		RawPayload:      raw,
	}

	// jobboard.Posting contains a []byte field so it isn't comparable with ==;
	// compare field by field instead.
	if got.SourceID != want.SourceID ||
		got.Title != want.Title ||
		got.Department != want.Department ||
		got.Team != want.Team ||
		got.Location != want.Location ||
		got.EmploymentType != want.EmploymentType ||
		got.WorkplaceType != want.WorkplaceType ||
		got.DescriptionHTML != want.DescriptionHTML ||
		got.DescriptionText != want.DescriptionText ||
		got.JobURL != want.JobURL ||
		got.ApplicationURL != want.ApplicationURL ||
		!got.PublishedAt.Equal(want.PublishedAt) ||
		string(got.RawPayload) != string(want.RawPayload) {
		t.Fatalf("sanitizePosting() = %+v, want %+v", got, want)
	}
}

func TestSanitizePosting_LeavesRawPayloadUntouched(t *testing.T) {
	p := jobboard.Posting{RawPayload: []byte(` {"padded": true} `)}

	got := sanitizePosting(p)

	if string(got.RawPayload) != ` {"padded": true} ` {
		t.Fatalf("RawPayload = %q, want untouched", string(got.RawPayload))
	}
}
