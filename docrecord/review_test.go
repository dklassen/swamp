package docrecord_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dklassen/swamp/docrecord"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
)

func TestDocumentType_MarshalJSON_UsesDBStringNotIntValue(t *testing.T) {
	got, err := json.Marshal(documents.Resume)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `"resume"`
	if string(got) != want {
		t.Fatalf("json.Marshal(documents.Resume) = %s, want %s (a bare int forces JSON consumers to know Swamp's internal enum ordering)", got, want)
	}
}

func TestOutcome_MarshalJSON_UsesDBStringNotIntValue(t *testing.T) {
	got, err := json.Marshal(docrecord.Flagged)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `"flagged"`
	if string(got) != want {
		t.Fatalf("json.Marshal(docrecord.Flagged) = %s, want %s (a bare int forces JSON consumers to know Swamp's internal enum ordering)", got, want)
	}
}

// TestDocumentType_MarshalJSON_AsMapKey_UsesDBStringNotIntValue guards
// against a real gotcha: encoding/json does NOT consult MarshalJSON for
// map keys, only encoding.TextMarshaler -- so a map[documents.Type]X would
// silently serialize keys as "0"/"1" (the underlying int) without a
// MarshalText method too, even with MarshalJSON already implemented and
// working correctly for every other position (see the two tests above).
func TestDocumentType_MarshalJSON_AsMapKey_UsesDBStringNotIntValue(t *testing.T) {
	m := map[documents.Type]bool{documents.CoverLetter: true}
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"cover_letter":true}`
	if string(got) != want {
		t.Fatalf("json.Marshal(map[documents.Type]bool{...}) = %s, want %s", got, want)
	}
}

func TestCreateReview_ThenList_ReturnsCreatedReview(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	created, err := createReview(ctx, s, application.ID, documents.CoverLetter, "Dear hiring manager...", docrecord.Passed, "Looks good")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	got, err := docrecord.ListReviews(ctx, s.DB(), application.ID, documents.CoverLetter)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}

	want := []docrecord.Review{created}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListReviews mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateReview_SetsCycleAndSHA256(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	content := "Dear hiring manager..."
	created, err := createReview(ctx, s, application.ID, documents.CoverLetter, content, docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	if created.Cycle != 1 {
		t.Fatalf("Cycle = %d, want 1 (first review)", created.Cycle)
	}
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	if created.ContentSHA256 != want {
		t.Fatalf("ContentSHA256 = %q, want %q", created.ContentSHA256, want)
	}
}

func TestReview_IsCurrent_MatchingContent_ReturnsTrue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	content := "Dear hiring manager..."
	created, err := createReview(ctx, s, application.ID, documents.CoverLetter, content, docrecord.Flagged, "too generic")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	if !created.IsCurrent(content) {
		t.Fatal("IsCurrent(content) = false, want true (content hasn't changed since the review)")
	}
}

func TestReview_IsCurrent_RevisedContent_ReturnsFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	created, err := createReview(ctx, s, application.ID, documents.CoverLetter, "Dear hiring manager...", docrecord.Flagged, "too generic")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	if created.IsCurrent("Dear hiring manager, revised with specifics...") {
		t.Fatal("IsCurrent(revised content) = true, want false -- the document has been rewritten since this review, it no longer describes what's on disk")
	}
}

func TestCreateReview_SecondReviewSameDocument_IncrementsCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft one", docrecord.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview (first): %v", err)
	}
	second, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft two", docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview (second): %v", err)
	}

	if second.Cycle != 2 {
		t.Fatalf("second review Cycle = %d, want 2", second.Cycle)
	}
}

func TestCreateReview_CycleIsPerDocumentType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := createReview(ctx, s, application.ID, documents.CoverLetter, "cover letter draft", docrecord.Passed, ""); err != nil {
		t.Fatalf("CreateReview (cover letter): %v", err)
	}
	resumeReview, err := createReview(ctx, s, application.ID, documents.Resume, "resume draft", docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview (resume): %v", err)
	}

	if resumeReview.Cycle != 1 {
		t.Fatalf("resume review Cycle = %d, want 1 -- cycle counts per document_type, not shared across cover letter/resume", resumeReview.Cycle)
	}
}

func TestLatestReview_NoReviews_ReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	_, ok, err := docrecord.LatestReview(ctx, s.DB(), application.ID, documents.CoverLetter)
	if err != nil {
		t.Fatalf("LatestReview: %v", err)
	}
	if ok {
		t.Fatal("ok = true, want false -- no reviews exist yet")
	}
}

func TestLatestReview_ReturnsMostRecentCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft one", docrecord.Flagged, "too generic"); err != nil {
		t.Fatalf("CreateReview (first): %v", err)
	}
	second, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft two", docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview (second): %v", err)
	}

	got, ok, err := docrecord.LatestReview(ctx, s.DB(), application.ID, documents.CoverLetter)
	if err != nil {
		t.Fatalf("LatestReview: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if diff := cmp.Diff(second, got); diff != "" {
		t.Fatalf("LatestReview mismatch (-want +got):\n%s", diff)
	}
}

func TestListReviews_ReturnsMostRecentCycleFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	first, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft one", docrecord.Flagged, "")
	if err != nil {
		t.Fatalf("CreateReview (first): %v", err)
	}
	second, err := createReview(ctx, s, application.ID, documents.CoverLetter, "draft two", docrecord.Passed, "")
	if err != nil {
		t.Fatalf("CreateReview (second): %v", err)
	}

	got, err := docrecord.ListReviews(ctx, s.DB(), application.ID, documents.CoverLetter)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}

	want := []docrecord.Review{second, first}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListReviews mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentTypes_ReturnsEveryDocumentTypeInOrder(t *testing.T) {
	t.Parallel()

	want := []documents.Type{documents.CoverLetter, documents.Resume}
	if diff := cmp.Diff(want, documents.Types()); diff != "" {
		t.Errorf("documents.Types() mismatch (-want +got):\n%s", diff)
	}
}

func TestOutcomes_EveryOutcomeInConstOrder(t *testing.T) {
	t.Parallel()

	want := []docrecord.Outcome{docrecord.Passed, docrecord.Flagged}
	if diff := cmp.Diff(want, docrecord.Outcomes()); diff != "" {
		t.Errorf("Outcomes() mismatch (-want +got):\n%s", diff)
	}
}
