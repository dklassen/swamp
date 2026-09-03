package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateDocumentReview_ThenList_ReturnsCreatedReview(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	created, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "Dear hiring manager...", ReviewOutcomePassed, "Looks good")
	if err != nil {
		t.Fatalf("CreateDocumentReview: %v", err)
	}

	got, err := s.ListDocumentReviews(ctx, application.ID, DocumentTypeCoverLetter)
	if err != nil {
		t.Fatalf("ListDocumentReviews: %v", err)
	}

	want := []DocumentReview{created}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDocumentReviews mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateDocumentReview_SetsCycleAndSHA256(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	content := "Dear hiring manager..."
	created, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, content, ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview: %v", err)
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

func TestCreateDocumentReview_SecondReviewSameDocument_IncrementsCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft one", ReviewOutcomeFlagged, "too generic"); err != nil {
		t.Fatalf("CreateDocumentReview (first): %v", err)
	}
	second, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft two", ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (second): %v", err)
	}

	if second.Cycle != 2 {
		t.Fatalf("second review Cycle = %d, want 2", second.Cycle)
	}
}

func TestCreateDocumentReview_CycleIsPerDocumentType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "cover letter draft", ReviewOutcomePassed, ""); err != nil {
		t.Fatalf("CreateDocumentReview (cover letter): %v", err)
	}
	resumeReview, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeResume, "resume draft", ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (resume): %v", err)
	}

	if resumeReview.Cycle != 1 {
		t.Fatalf("resume review Cycle = %d, want 1 -- cycle counts per document_type, not shared across cover letter/resume", resumeReview.Cycle)
	}
}

func TestLatestDocumentReview_NoReviews_ReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	_, ok, err := s.LatestDocumentReview(ctx, application.ID, DocumentTypeCoverLetter)
	if err != nil {
		t.Fatalf("LatestDocumentReview: %v", err)
	}
	if ok {
		t.Fatal("ok = true, want false -- no reviews exist yet")
	}
}

func TestLatestDocumentReview_ReturnsMostRecentCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft one", ReviewOutcomeFlagged, "too generic"); err != nil {
		t.Fatalf("CreateDocumentReview (first): %v", err)
	}
	second, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft two", ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (second): %v", err)
	}

	got, ok, err := s.LatestDocumentReview(ctx, application.ID, DocumentTypeCoverLetter)
	if err != nil {
		t.Fatalf("LatestDocumentReview: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if diff := cmp.Diff(second, got); diff != "" {
		t.Fatalf("LatestDocumentReview mismatch (-want +got):\n%s", diff)
	}
}

func TestListDocumentReviews_ReturnsMostRecentCycleFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	first, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft one", ReviewOutcomeFlagged, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (first): %v", err)
	}
	second, err := s.CreateDocumentReview(ctx, application.ID, DocumentTypeCoverLetter, "draft two", ReviewOutcomePassed, "")
	if err != nil {
		t.Fatalf("CreateDocumentReview (second): %v", err)
	}

	got, err := s.ListDocumentReviews(ctx, application.ID, DocumentTypeCoverLetter)
	if err != nil {
		t.Fatalf("ListDocumentReviews: %v", err)
	}

	want := []DocumentReview{second, first}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListDocumentReviews mismatch (-want +got):\n%s", diff)
	}
}
