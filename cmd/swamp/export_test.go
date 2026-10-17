package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
)

func TestExportDocumentPDF_RecordsTheExport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	documentService := service.NewDocumentService(testutil.NewTestStore(t))

	const content = "# Resume\n\nExperience.\n"
	mdPath := filepath.Join(t.TempDir(), "resume.md")
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	outPath, err := exportDocumentPDF(ctx, documentService, 7, document.Resume, mdPath)
	if err != nil {
		t.Fatalf("exportDocumentPDF: %v", err)
	}

	exports, err := documentService.LatestExports(ctx, 7)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	resume, ok := exports[document.Resume]
	if !ok {
		t.Fatal("no resume export recorded")
	}
	if resume.Path != outPath || !resume.IsCurrent(content) {
		t.Errorf("recorded export = %+v, want path %q and current for the exported content", resume, outPath)
	}
}

func TestExportDocumentPDF_FailedExportRecordsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	documentService := service.NewDocumentService(testutil.NewTestStore(t))

	if _, err := exportDocumentPDF(ctx, documentService, 7, document.Resume, filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("exportDocumentPDF succeeded for a missing document, want an error")
	}

	exports, err := documentService.LatestExports(ctx, 7)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	if len(exports) != 0 {
		t.Errorf("exports = %+v, want none recorded for a failed export", exports)
	}
}

func TestReviewSummary(t *testing.T) {
	t.Parallel()

	reviewedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		review    document.Review
		hasReview bool
		want      string
	}{
		// Presence comes from the map lookup, never the review's fields:
		// these two would flip if CreatedAt were still the check.
		{"no review, despite a timestamp", document.Review{CreatedAt: reviewedAt}, false, "not yet reviewed"},
		{"passed, without a timestamp", document.Review{Outcome: document.Passed}, true, "passed"},
		{"no review", document.Review{}, false, "not yet reviewed"},
		{"flagged", document.Review{Outcome: document.Flagged, Notes: "too long", CreatedAt: reviewedAt}, true, "flagged: too long"},
		{"passed", document.Review{Outcome: document.Passed, CreatedAt: reviewedAt}, true, "passed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reviewSummary(tc.review, tc.hasReview); got != tc.want {
				t.Errorf("reviewSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}
