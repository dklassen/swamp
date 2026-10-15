package docrecord_test

import (
	"context"
	"github.com/dklassen/swamp/docrecord"
	"testing"

	"github.com/dklassen/swamp/documents"
)

func TestWrites_LatestIsTheNewestOfThatDocument(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	application, err := createApplication(ctx, s, posting.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}

	if _, ok, err := docrecord.LatestWrite(ctx, s.DB(), application.ID, documents.CoverLetter); err != nil || ok {
		t.Fatalf("LatestWrite before any write = ok %v, err %v; want none", ok, err)
	}
	for _, w := range []struct {
		documentType documents.Type
		content      string
		source       docrecord.Source
	}{
		{documents.CoverLetter, "first", docrecord.SourceWriteDocument},
		{documents.CoverLetter, "second", docrecord.SourceEditor},
		{documents.Resume, "a resume", docrecord.SourceWriteDocument},
	} {
		if err := docrecord.RecordWrite(ctx, s.DB(), application.ID, w.documentType, w.content, w.source); err != nil {
			t.Fatalf("RecordWrite: %v", err)
		}
	}

	got, ok, err := docrecord.LatestWrite(ctx, s.DB(), application.ID, documents.CoverLetter)
	if err != nil || !ok {
		t.Fatalf("LatestWrite = ok %v, err %v; want the second write", ok, err)
	}
	if got.Source != docrecord.SourceEditor || got.ContentSHA256 != documents.ContentSHA256("second") {
		t.Errorf("latest cover letter write = source %q, hash %q; want editor, hash of %q", got.Source, got.ContentSHA256, "second")
	}
	if got.WrittenAt.IsZero() {
		t.Error("WrittenAt is zero, want when it was recorded")
	}
}
