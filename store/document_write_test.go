package store

import (
	"context"
	"testing"

	"github.com/dklassen/swamp/documents"
)

func TestDocumentWrites_LatestIsTheNewestOfThatDocument(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application, err := s.CreateApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	if _, ok, err := s.LatestDocumentWrite(ctx, application.ID, documents.CoverLetter); err != nil || ok {
		t.Fatalf("LatestDocumentWrite before any write = ok %v, err %v; want none", ok, err)
	}
	for _, w := range []struct {
		documentType documents.Type
		content      string
		source       DocumentWriteSource
	}{
		{documents.CoverLetter, "first", DocumentWriteSourceWriteDocument},
		{documents.CoverLetter, "second", DocumentWriteSourceEditor},
		{documents.Resume, "a resume", DocumentWriteSourceWriteDocument},
	} {
		if err := s.RecordDocumentWrite(ctx, application.ID, w.documentType, w.content, w.source); err != nil {
			t.Fatalf("RecordDocumentWrite: %v", err)
		}
	}

	got, ok, err := s.LatestDocumentWrite(ctx, application.ID, documents.CoverLetter)
	if err != nil || !ok {
		t.Fatalf("LatestDocumentWrite = ok %v, err %v; want the second write", ok, err)
	}
	if got.Source != DocumentWriteSourceEditor || got.ContentSHA256 != documents.ContentSHA256("second") {
		t.Errorf("latest cover letter write = source %q, hash %q; want editor, hash of %q", got.Source, got.ContentSHA256, "second")
	}
	if got.WrittenAt.IsZero() {
		t.Error("WrittenAt is zero, want when it was recorded")
	}
}
