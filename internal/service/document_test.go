package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/store"
)

// A review's cycle counts the reviews of that document for that
// application, so the cover letter's reviews don't move the resume's.
func TestDocumentService_Review_CycleCountsPerDocumentType(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	var got []int64
	for _, documentType := range []document.Type{document.CoverLetter, document.CoverLetter, document.Resume} {
		review, err := documents.Review(ctx, app.ID, documentType, "draft", document.Passed, "")
		if err != nil {
			t.Fatalf("Review(%s): %v", documentType, err)
		}
		got = append(got, review.Cycle)
	}
	if want := []int64{1, 2, 1}; !slices.Equal(got, want) {
		t.Errorf("cycles of cover letter, cover letter, resume = %v, want %v", got, want)
	}
}

// The snapshot keeps a review readable after the file is redrafted in
// place, and the hash says whether the file changed since.
func TestDocumentService_Review_RecordsTheContentAndItsSHA256(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	content := "Dear hiring manager..."

	review, err := s.Documents.Review(context.Background(), app.ID, document.CoverLetter, content, document.Passed, "")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if review.ContentSnapshot != content {
		t.Errorf("ContentSnapshot = %q, want %q", review.ContentSnapshot, content)
	}
	sum := sha256.Sum256([]byte(content))
	if want := hex.EncodeToString(sum[:]); review.ContentSHA256 != want {
		t.Errorf("ContentSHA256 = %q, want %q", review.ContentSHA256, want)
	}
}

// IsCurrent hashes content the way Review did when it recorded the
// review; if the two drifted, every review would read as out of date.
func TestDocumentService_Review_IsCurrentUntilTheContentChanges(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	content := "Dear hiring manager..."
	if _, err := documents.Review(ctx, app.ID, document.CoverLetter, content, document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	review, err := documents.LatestReview(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("LatestReview: %v", err)
	}
	if !review.IsCurrent(content) {
		t.Error("IsCurrent(the reviewed content) = false, want true")
	}
	if review.IsCurrent("Dear hiring manager, revised with specifics...") {
		t.Error("IsCurrent(revised content) = true, want false: the review describes a version no longer on disk")
	}
}

func TestDocumentService_Reviews_MostRecentCycleFirst(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	first, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft one", document.Flagged, "")
	if err != nil {
		t.Fatalf("Review (first): %v", err)
	}
	second, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft two", document.Passed, "")
	if err != nil {
		t.Fatalf("Review (second): %v", err)
	}

	got, err := documents.Reviews(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if diff := cmp.Diff([]document.Review{second, first}, got); diff != "" {
		t.Errorf("Reviews (-want +got):\n%s", diff)
	}
}

func TestDocumentService_LatestReview_NoReviews_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	got, err := s.Documents.LatestReview(context.Background(), app.ID, document.CoverLetter)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("LatestReview with no reviews: err = %v, want store.ErrNotFound", err)
	}
	if got != nil {
		t.Errorf("LatestReview with no reviews = %+v, want nil", got)
	}
}

func TestDocumentService_LatestReview_ReturnsTheMostRecentCycle(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	if _, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft one", document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review (first): %v", err)
	}
	second, err := documents.Review(ctx, app.ID, document.CoverLetter, "draft two", document.Passed, "")
	if err != nil {
		t.Fatalf("Review (second): %v", err)
	}

	got, err := documents.LatestReview(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("LatestReview: %v", err)
	}
	if diff := cmp.Diff(&second, got); diff != "" {
		t.Errorf("LatestReview (-want +got):\n%s", diff)
	}
}

func TestDocumentService_LatestExports_NewestPerTypeIsCurrentUntilTheContentChanges(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	const v1, v2 = "# Cover letter v1", "# Cover letter v2"
	if err := documents.RecordExport(ctx, app.ID, document.CoverLetter, v1, "/out/v1-cover_letter.pdf"); err != nil {
		t.Fatalf("RecordExport v1: %v", err)
	}
	const latestPath = "/out/v2-cover_letter.pdf"
	if err := documents.RecordExport(ctx, app.ID, document.CoverLetter, v2, latestPath); err != nil {
		t.Fatalf("RecordExport v2: %v", err)
	}

	exports, err := documents.LatestExports(ctx, app.ID)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	if _, ok := exports[document.Resume]; ok {
		t.Error("LatestExports has a resume export, want none: the resume was never exported")
	}
	coverLetter, ok := exports[document.CoverLetter]
	if !ok {
		t.Fatal("LatestExports has no cover letter export")
	}
	if coverLetter.Path != latestPath || coverLetter.ExportedAt.IsZero() {
		t.Errorf("latest cover letter export = %+v, want the v2 export with an ExportedAt", coverLetter)
	}
	for content, want := range map[string]bool{v2: true, v1: false, "# Cover letter v3": false} {
		if got := coverLetter.IsCurrent(content); got != want {
			t.Errorf("IsCurrent(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestDocumentService_LatestWrite_IsTheNewestOfThatDocument(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	documents := s.Documents
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	none, err := documents.LatestWrite(ctx, app.ID, document.CoverLetter)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("LatestWrite before any write: err = %v, want store.ErrNotFound", err)
	}
	if none != nil {
		t.Errorf("LatestWrite before any write = %+v, want nil", none)
	}
	for _, w := range []struct {
		documentType document.Type
		content      string
		source       document.Source
	}{
		{document.CoverLetter, "first", document.SourceWriteDocument},
		{document.CoverLetter, "second", document.SourceEditor},
		{document.Resume, "a resume", document.SourceWriteDocument},
	} {
		if err := documents.RecordWrite(ctx, app.ID, w.documentType, w.content, w.source); err != nil {
			t.Fatalf("RecordWrite: %v", err)
		}
	}

	got, err := documents.LatestWrite(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("LatestWrite: %v", err)
	}
	sum := sha256.Sum256([]byte("second"))
	if want := hex.EncodeToString(sum[:]); got.Source != document.SourceEditor || got.ContentSHA256 != want {
		t.Errorf("latest cover letter write = source %s, hash %q; want %s, hash %q", got.Source, got.ContentSHA256, document.SourceEditor, want)
	}
	if got.WrittenAt.IsZero() {
		t.Error("WrittenAt is zero, want when it was recorded")
	}
}
