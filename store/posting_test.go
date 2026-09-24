package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestUpsertPosting_NewPosting_ThenGet_ReturnsSamePosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	got, err := s.GetPosting(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPosting mismatch (-created +got):\n%s", diff)
	}
}

func TestUpsertPosting_NewPosting_AutoCreatesMarkupRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	created, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	markup, err := s.GetPostingMarkup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPostingMarkup: %v", err)
	}

	want := PostingMarkup{
		PostingID: created.ID,
		Notes:     "",
		CreatedAt: markup.CreatedAt,
		UpdatedAt: markup.UpdatedAt,
	}
	if diff := cmp.Diff(want, markup); diff != "" {
		t.Fatalf("GetPostingMarkup mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertPosting_SameSourceAndSourceID_UpdatesInPlace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	first, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting (first): %v", err)
	}

	second, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Senior Software Engineer",
			RawPayload: `{"id":"job-1","title":"Senior Software Engineer"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting (second): %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("UpsertPosting (second).ID = %d, want %d (same posting)", second.ID, first.ID)
	}
	if second.Title != "Senior Software Engineer" {
		t.Fatalf("UpsertPosting (second).Title = %q, want %q", second.Title, "Senior Software Engineer")
	}

	all, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListPostingsByCompany = %d postings, want 1 (upsert should not duplicate)", len(all))
	}
}

func TestUpsertPosting_OnUpdate_DoesNotChangeListingStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	updated, err := s.UpsertPosting(ctx, CreatePostingParams{
		CompanyID: acme.ID,
		Source:    "ashby",
		SourceID:  "job-1",
		IngestedFields: IngestedFields{
			Title:      "Software Engineer II",
			RawPayload: `{"id":"job-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	if updated.ListingStatus != "closed" {
		t.Fatalf("UpsertPosting.ListingStatus = %q, want %q (upsert must not reopen)", updated.ListingStatus, "closed")
	}
}

func TestGetPosting_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPosting(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPosting error = %v, want ErrNotFound", err)
	}
}

func TestGetPostingBySourceAndSourceID_ReturnsMatchingPosting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	created := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	got, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "job-1")
	if err != nil {
		t.Fatalf("GetPostingBySourceAndSourceID: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetPostingBySourceAndSourceID mismatch (-created +got):\n%s", diff)
	}
}

func TestGetPostingBySourceAndSourceID_NoMatch_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingBySourceAndSourceID(ctx, "ashby", "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingBySourceAndSourceID error = %v, want ErrNotFound", err)
	}
}

func TestListPostingsByCompany_OnlyReturnsPostingsForThatCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	acmePosting := mustUpsertPosting(t, s, acme.ID, "job-1", "Acme Engineer")
	mustUpsertPosting(t, s, globex.ID, "job-2", "Globex Engineer")

	got, err := s.ListPostingsByCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}

	want := []Posting{acmePosting}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListPostingsByCompany mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkPostingClosed_SetsListingStatusClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "closed" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "closed")
	}
}

func TestMarkPostingReopened_SetsListingStatusOpen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if err := s.MarkPostingReopened(ctx, posting.ID); err != nil {
		t.Fatalf("MarkPostingReopened: %v", err)
	}

	got, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if got.ListingStatus != "open" {
		t.Fatalf("ListingStatus = %q, want %q", got.ListingStatus, "open")
	}
}

func TestGetPostingMarkup_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPostingMarkup(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPostingMarkup error = %v, want ErrNotFound", err)
	}
}

func TestUnmarkPostingInterested_ClearsInterestedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}

	updated, err := s.UnmarkPostingInterested(ctx, posting.ID)
	if err != nil {
		t.Fatalf("UnmarkPostingInterested: %v", err)
	}
	if updated.InterestedAt != nil {
		t.Fatalf("InterestedAt = %v, want nil", updated.InterestedAt)
	}
}

func TestUnarchivePosting_ClearsArchivedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	if _, err := s.SetPostingArchived(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	updated, err := s.UnarchivePosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("UnarchivePosting: %v", err)
	}
	if updated.ArchivedAt != nil {
		t.Fatalf("ArchivedAt = %v, want nil", updated.ArchivedAt)
	}
}

func TestUpdatePostingMarkupNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	updated, err := s.UpdatePostingMarkupNotes(ctx, posting.ID, "Looks like a great fit")
	if err != nil {
		t.Fatalf("UpdatePostingMarkupNotes: %v", err)
	}
	if updated.Notes != "Looks like a great fit" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Looks like a great fit")
	}
}

func TestListDistinctDepartmentsForCompany_ReturnsSortedUniqueValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	eng := "Engineering"
	sales := "Sales"
	for i, dept := range []string{eng, sales, eng} {
		_, err := s.UpsertPosting(ctx, CreatePostingParams{
			CompanyID: acme.ID,
			Source:    "ashby",
			SourceID:  fmt.Sprintf("job-%d", i),
			IngestedFields: IngestedFields{
				Title:      "Role",
				Department: dept,
				RawPayload: "{}",
			},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
	}

	got, err := s.ListDistinctDepartmentsForCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListDistinctDepartmentsForCompany: %v", err)
	}
	want := []string{"Engineering", "Sales"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDistinctDepartmentsForCompany mismatch (-want +got):\n%s", diff)
	}
}

func TestListDistinctLocationsForCompany_ReturnsSortedUniqueValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	remote := "Remote"
	nyc := "New York"
	for i, loc := range []string{nyc, remote, remote} {
		_, err := s.UpsertPosting(ctx, CreatePostingParams{
			CompanyID: acme.ID,
			Source:    "ashby",
			SourceID:  fmt.Sprintf("job-%d", i),
			IngestedFields: IngestedFields{
				Title:      "Role",
				Location:   loc,
				RawPayload: "{}",
			},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
	}

	got, err := s.ListDistinctLocationsForCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("ListDistinctLocationsForCompany: %v", err)
	}
	want := []string{"New York", "Remote"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDistinctLocationsForCompany mismatch (-want +got):\n%s", diff)
	}
}

// TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull covers #80.
// PublishedAt is an OptionalTime, whose zero value means "not known"
// (it is not a pointer -- see #67, nothing in Go distinguishes never-set
// from the zero value). The consumer of this JSON is an external agent,
// and "0001-01-01T00:00:00Z" is noise in that contract where null is a
// clean absence. Also catches OptionalTime.MarshalJSON being removed,
// which would silently degrade to the embedded time.Time's own.
func TestPosting_MarshalJSON_ZeroPublishedAtSerializesAsNull(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Posting{ID: 1})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":null`)) {
		t.Errorf("Posting with no publish date encoded as %s, want \"PublishedAt\":null", encoded)
	}
	// Scoped to PublishedAt: FirstSeenAt/CreatedAt/UpdatedAt are NOT NULL
	// columns that are always set in practice, so their zero value in a
	// synthetic struct is not the contract problem this fixes.
	if bytes.Contains(encoded, []byte(`"PublishedAt":"0001-01-01`)) {
		t.Errorf("Posting encoded as %s, want no zero-time sentinel for PublishedAt in the agent contract", encoded)
	}
}

func TestPosting_MarshalJSON_RealPublishedAtIsPreserved(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 8, 24, 13, 15, 0, 0, time.UTC)
	encoded, err := json.Marshal(Posting{ID: 1, IngestedFields: IngestedFields{PublishedAt: OptionalTime{Time: published}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"PublishedAt":"2026-08-24T13:15:00Z"`)) {
		t.Errorf("Posting encoded as %s, want the real publish date preserved", encoded)
	}
}

// TestPosting_MarshalJSON_KeepsEveryField guards against a struct-level
// MarshalJSON reappearing on Posting or on the IngestedFields it embeds.
// IngestedFields is embedded untagged so its fields stay promoted into
// Posting's JSON object (#59); a MarshalJSON on it would be promoted too
// and hijack the whole object, dropping ID, ListingStatus and the
// timestamps from the agent contract with no error. Encoding the
// optionality on OptionalTime instead of on a struct is what avoids
// that, and this test fails if anyone reintroduces the struct-level form.
func TestPosting_MarshalJSON_KeepsEveryField(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Posting{ID: 7, IngestedFields: IngestedFields{Title: "Engineer"}})
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

// Open means what a company's posting list shows by default: listings
// still open on the job board that the user hasn't archived.
func TestCountOpenPostingsByCompany_CountsOpenUnarchivedPerCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	closed := mustUpsertPosting(t, s, acme.ID, "job-3", "Closed role")
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	archived := mustUpsertPosting(t, s, acme.ID, "job-4", "Archived role")
	if _, err := s.SetPostingArchived(ctx, archived.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}

	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")
	mustUpsertPosting(t, s, globex.ID, "globex-job-1", "Engineer")

	initech := mustCreateCompany(t, s, "Initech", "ashby", "initech")

	got, err := s.CountOpenPostingsByCompany(ctx)
	if err != nil {
		t.Fatalf("CountOpenPostingsByCompany: %v", err)
	}
	want := map[int64]int{acme.ID: 2, globex.ID: 1}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("counts mismatch (-want +got):\n%s", diff)
	}
	if n := got[initech.ID]; n != 0 {
		t.Errorf("Initech (no postings) count = %d, want 0", n)
	}
}
