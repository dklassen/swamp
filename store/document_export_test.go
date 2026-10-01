package store

import (
	"context"
	"testing"

	"github.com/dklassen/swamp/documents"
)

func TestRecordDocumentExport_LatestPerTypeIsCurrentUntilContentChanges(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	for _, export := range []struct {
		documentType documents.Type
		content      string
		path         string
	}{
		{documents.CoverLetter, "# Cover letter v1", "/out/v1-cover_letter.pdf"},
		{documents.CoverLetter, "# Cover letter v2", "/out/v2-cover_letter.pdf"},
	} {
		if err := s.RecordDocumentExport(ctx, application.ID, export.documentType, export.content, export.path); err != nil {
			t.Fatalf("RecordDocumentExport: %v", err)
		}
	}

	exports, err := s.LatestDocumentExports(ctx, application.ID)
	if err != nil {
		t.Fatalf("LatestDocumentExports: %v", err)
	}
	if _, ok := exports[documents.Resume]; ok {
		t.Errorf("LatestDocumentExports has a resume export, want none: the resume was never exported")
	}
	coverLetter, ok := exports[documents.CoverLetter]
	if !ok {
		t.Fatal("LatestDocumentExports has no cover letter export")
	}
	if coverLetter.Path != "/out/v2-cover_letter.pdf" || coverLetter.ExportedAt.IsZero() {
		t.Errorf("latest cover letter export = %+v, want the v2 export with an ExportedAt", coverLetter)
	}

	tests := []struct {
		content string
		want    bool
	}{
		{content: "# Cover letter v2", want: true},
		{content: "# Cover letter v1", want: false},
		{content: "# Cover letter v3", want: false},
	}
	for _, tt := range tests {
		if got := coverLetter.IsCurrent(tt.content); got != tt.want {
			t.Errorf("IsCurrent(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}
