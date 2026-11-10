package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/store"
)

func TestDraftingService_Prepare_FirstCall_StartsTheApplicationAndItsFolder(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.Application.PostingID != p.ID || got.Application.Status != application.Started {
		t.Errorf("Application = %+v, want a started application for posting %d", got.Application, p.ID)
	}
	stored, err := s.Applications.Get(t.Context(), got.Application.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(*stored, got.Application); diff != "" {
		t.Errorf("Application against the stored one (-stored +got):\n%s", diff)
	}
	for _, documentType := range document.Types() {
		dir := filepath.Dir(got.Documents[documentType].Path)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s's folder %q: stat err = %v, want a directory", documentType, dir, err)
		}
	}
}

func TestDraftingService_Prepare_SecondCall_ResumesTheSameApplication(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	first, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (first): %v", err)
	}

	second, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (second): %v", err)
	}
	if diff := cmp.Diff(first.Application, second.Application); diff != "" {
		t.Errorf("second call's application (-first +second):\n%s", diff)
	}
	if diff := cmp.Diff(first.Documents, second.Documents); diff != "" {
		t.Errorf("second call's documents (-first +second):\n%s", diff)
	}
}

func TestDraftingService_Prepare_ClosedPosting_IsRefused(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	if err := s.Postings.Close(t.Context(), p.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if !errors.Is(err, service.ErrPostingClosed) {
		t.Fatalf("Prepare = %+v, %v; want ErrPostingClosed", got, err)
	}
	if _, err := s.Postings.Application(t.Context(), p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Application after a refused Prepare: err = %v, want store.ErrNotFound", err)
	}
}

func TestDraftingService_Prepare_PostingAtADeletedCompany_IsRefused(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "Acme")
	p := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
	if _, err := s.Companies.Delete(t.Context(), acme.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	var deleted *service.CompanyDeletedError
	if !errors.As(err, &deleted) {
		t.Fatalf("Prepare = %+v, %v; want a CompanyDeletedError", got, err)
	}
	if want := (service.CompanyDeletedError{PostingID: p.ID, CompanyName: "Acme"}); *deleted != want {
		t.Errorf("CompanyDeletedError = %+v, want %+v", *deleted, want)
	}
}

func TestDraftingService_Prepare_DocumentsAreVersionedByWhatsOnDisk(t *testing.T) {
	t.Parallel()
	const resume = "# Resume"
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	first, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (first): %v", err)
	}
	if _, err := s.Documents.Write(t.Context(), first.Application.ID, document.Resume, resume, service.NoDocumentYet, document.SourceWriteDocument); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare (second): %v", err)
	}
	want := map[document.Type]service.DraftDocument{
		document.CoverLetter: {Path: first.Documents[document.CoverLetter].Path, Exists: false, Version: service.NoDocumentYet},
		document.Resume:      {Path: first.Documents[document.Resume].Path, Exists: true, Version: document.ContentSHA256(resume)},
	}
	if diff := cmp.Diff(want, got.Documents); diff != "" {
		t.Errorf("Documents (-want +got):\n%s", diff)
	}
}

// A review counts only while it describes the document on disk.
func TestDraftingService_Prepare_ReviewsOnlyOfTheDocumentOnDisk(t *testing.T) {
	t.Parallel()
	const reviewed = "Dear hiring manager..."
	tests := []struct {
		name string
		// rewrite is the cover letter written after the review; "" writes none.
		rewrite    string
		wantReview bool
	}{
		{name: "document unchanged", rewrite: "", wantReview: true},
		{name: "document rewritten", rewrite: "Dear hiring manager, revised...", wantReview: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s := testutil.NewTestApp(t)
			p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
			first, err := s.Drafting.Prepare(ctx, p.ID)
			if err != nil {
				t.Fatalf("Prepare (first): %v", err)
			}
			appID := first.Application.ID
			if _, err := s.Documents.Write(ctx, appID, document.CoverLetter, reviewed, service.NoDocumentYet, document.SourceWriteDocument); err != nil {
				t.Fatalf("Write: %v", err)
			}
			review, err := s.Documents.Review(ctx, appID, document.CoverLetter, reviewed, document.Flagged, "too generic")
			if err != nil {
				t.Fatalf("Review: %v", err)
			}
			if tt.rewrite != "" {
				if _, err := s.Documents.Write(ctx, appID, document.CoverLetter, tt.rewrite, document.ContentSHA256(reviewed), document.SourceWriteDocument); err != nil {
					t.Fatalf("Write (rewrite): %v", err)
				}
			}

			got, err := s.Drafting.Prepare(ctx, p.ID)
			if err != nil {
				t.Fatalf("Prepare (second): %v", err)
			}
			want := map[document.Type]document.Review{}
			if tt.wantReview {
				want[document.CoverLetter] = *review
			}
			if diff := cmp.Diff(want, got.LatestReviews); diff != "" {
				t.Errorf("LatestReviews (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDraftingService_Prepare_ApplicationStartedElsewhere_IsReturned(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	started := testutil.MustApplyTo(t, s, p.ID)

	got, err := s.Drafting.Prepare(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := cmp.Diff(started, got.Application); diff != "" {
		t.Errorf("Application (-started +got):\n%s", diff)
	}
}
