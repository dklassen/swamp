package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func newExportTestStore(t *testing.T) *store.Store {
	t.Helper()
	return newExportTestStoreAt(t, filepath.Join(t.TempDir(), "test.db"))
}

// newExportTestStoreAt is newExportTestStore on the database at path.
func newExportTestStoreAt(t *testing.T, path string) *store.Store {
	t.Helper()
	sqlDB, err := store.Open(path, store.DefaultConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return store.New(sqlDB)
}

func TestExportDocumentPDF_RecordsTheExport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newExportTestStore(t)

	const content = "# Resume\n\nExperience.\n"
	mdPath := filepath.Join(t.TempDir(), "resume.md")
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	outPath, err := exportDocumentPDF(ctx, s, 7, documents.Resume, mdPath)
	if err != nil {
		t.Fatalf("exportDocumentPDF: %v", err)
	}

	exports, err := s.LatestDocumentExports(ctx, 7)
	if err != nil {
		t.Fatalf("LatestDocumentExports: %v", err)
	}
	resume, ok := exports[documents.Resume]
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
	s := newExportTestStore(t)

	if _, err := exportDocumentPDF(ctx, s, 7, documents.Resume, filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("exportDocumentPDF succeeded for a missing document, want an error")
	}

	exports, err := s.LatestDocumentExports(ctx, 7)
	if err != nil {
		t.Fatalf("LatestDocumentExports: %v", err)
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
		review    store.DocumentReview
		hasReview bool
		want      string
	}{
		// Presence comes from the map lookup, never the review's fields:
		// these two would flip if CreatedAt were still the check.
		{"no review, despite a timestamp", store.DocumentReview{CreatedAt: reviewedAt}, false, "not yet reviewed"},
		{"passed, without a timestamp", store.DocumentReview{Outcome: store.ReviewOutcomePassed}, true, "passed"},
		{"no review", store.DocumentReview{}, false, "not yet reviewed"},
		{"flagged", store.DocumentReview{Outcome: store.ReviewOutcomeFlagged, Notes: "too long", CreatedAt: reviewedAt}, true, "flagged: too long"},
		{"passed", store.DocumentReview{Outcome: store.ReviewOutcomePassed, CreatedAt: reviewedAt}, true, "passed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reviewSummary(tc.review, tc.hasReview); got != tc.want {
				t.Errorf("reviewSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}
