package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/service"
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
	review, err := documents.Review(ctx, app.ID, document.CoverLetter, content, document.Flagged, "too generic")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	if !review.IsCurrent(content) {
		t.Error("IsCurrent(the reviewed content) = false, want true")
	}
	if review.IsCurrent("Dear hiring manager, revised with specifics...") {
		t.Error("IsCurrent(revised content) = true, want false: the review describes a version no longer on disk")
	}
}

// A review counts only while it describes the document on disk: one of
// an earlier draft, or of a document that isn't there, is no review.
func TestDocumentService_LatestReviews_OnlyOfTheDocumentOnDisk(t *testing.T) {
	t.Parallel()
	const reviewed = "Dear hiring manager..."
	tests := []struct {
		name string
		// onDisk is the cover letter written after the review; "" writes none.
		onDisk     string
		wantReview bool
	}{
		{name: "document unchanged", onDisk: reviewed, wantReview: true},
		{name: "document rewritten", onDisk: "Dear hiring manager, revised...", wantReview: false},
		{name: "no document", onDisk: "", wantReview: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			review, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, reviewed, document.Flagged, "too generic")
			if err != nil {
				t.Fatalf("Review: %v", err)
			}
			if tt.onDisk != "" {
				if _, err := s.Docs.Write(app.ID, document.CoverLetter, tt.onDisk, ""); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}

			got, err := s.Documents.LatestReviews(ctx, app.ID)
			if err != nil {
				t.Fatalf("LatestReviews: %v", err)
			}
			want := map[document.Type]document.Review{}
			if tt.wantReview {
				want[document.CoverLetter] = *review
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("LatestReviews (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDocumentService_LatestReviews_TheLatestOfEachType(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	if _, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, "letter one", document.Flagged, "too generic"); err != nil {
		t.Fatalf("Review (first letter): %v", err)
	}
	letter, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, "letter two", document.Passed, "")
	if err != nil {
		t.Fatalf("Review (second letter): %v", err)
	}
	resume, err := s.Documents.Review(ctx, app.ID, document.Resume, "resume one", document.Flagged, "too long")
	if err != nil {
		t.Fatalf("Review (resume): %v", err)
	}
	for documentType, content := range map[document.Type]string{document.CoverLetter: "letter two", document.Resume: "resume one"} {
		if _, err := s.Docs.Write(app.ID, documentType, content, ""); err != nil {
			t.Fatalf("Write %s: %v", documentType, err)
		}
	}

	got, err := s.Documents.LatestReviews(ctx, app.ID)
	if err != nil {
		t.Fatalf("LatestReviews: %v", err)
	}
	want := map[document.Type]document.Review{document.CoverLetter: *letter, document.Resume: *resume}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LatestReviews (-want +got):\n%s", diff)
	}
}

func TestDocumentService_LatestReviews_OnlyTheApplicationsOwn(t *testing.T) {
	t.Parallel()
	const letter = "Dear hiring manager..."
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	other := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-2", "Platform Engineer").ID)
	if _, err := s.Docs.Write(app.ID, document.CoverLetter, letter, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.Documents.Review(ctx, other.ID, document.CoverLetter, letter, document.Passed, ""); err != nil {
		t.Fatalf("Review: %v", err)
	}

	got, err := s.Documents.LatestReviews(ctx, app.ID)
	if err != nil {
		t.Fatalf("LatestReviews: %v", err)
	}
	if diff := cmp.Diff(map[document.Type]document.Review{}, got); diff != "" {
		t.Errorf("LatestReviews (-want +got):\n%s", diff)
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

func TestDocumentService_GetManyReviews_LeavesOutMissing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	cover, err := s.Documents.Review(ctx, app.ID, document.CoverLetter, "Dear Acme", document.Passed, "")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	resume, err := s.Documents.Review(ctx, app.ID, document.Resume, "Experience", document.Flagged, "too long")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	got, err := s.Documents.GetManyReviews(ctx, []int64{resume.ID, 9999, cover.ID})
	if err != nil {
		t.Fatalf("GetManyReviews: %v", err)
	}
	want := []document.Review{*cover, *resume}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b document.Review) bool { return a.ID < b.ID })); diff != "" {
		t.Errorf("GetManyReviews (-want +got):\n%s", diff)
	}
}

func TestDocumentService_GetManyWrites_LeavesOutMissing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	var want []document.Write
	ids := []int64{9999}
	for _, documentType := range []document.Type{document.CoverLetter, document.Resume} {
		if err := s.Documents.RecordWrite(ctx, app.ID, documentType, "draft", document.SourceWriteDocument); err != nil {
			t.Fatalf("RecordWrite: %v", err)
		}
		w, err := s.Documents.LatestWrite(ctx, app.ID, documentType)
		if err != nil {
			t.Fatalf("LatestWrite: %v", err)
		}
		want = append(want, *w)
		ids = append(ids, w.ID)
	}

	got, err := s.Documents.GetManyWrites(ctx, ids)
	if err != nil {
		t.Fatalf("GetManyWrites: %v", err)
	}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b document.Write) bool { return a.ID < b.ID })); diff != "" {
		t.Errorf("GetManyWrites (-want +got):\n%s", diff)
	}
}

func TestDocumentService_GetManyExports_LeavesOutMissing(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	for _, documentType := range []document.Type{document.CoverLetter, document.Resume} {
		if err := s.Documents.RecordExport(ctx, app.ID, documentType, "draft", "/out/"+documentType.String()+".pdf"); err != nil {
			t.Fatalf("RecordExport: %v", err)
		}
	}
	latest, err := s.Documents.LatestExports(ctx, app.ID)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	var want []document.Export
	ids := []int64{9999}
	for _, e := range latest {
		want = append(want, e)
		ids = append(ids, e.ID)
	}

	got, err := s.Documents.GetManyExports(ctx, ids)
	if err != nil {
		t.Fatalf("GetManyExports: %v", err)
	}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b document.Export) bool { return a.ID < b.ID })); diff != "" {
		t.Errorf("GetManyExports (-want +got):\n%s", diff)
	}
}

func TestDocumentService_Write_WritesAndRecordsTheWrite(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	path, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "a draft", service.NoDocumentYet, document.SourceWriteDocument)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(onDisk) != "a draft" {
		t.Errorf("document at %s = %q, want %q", path, onDisk, "a draft")
	}
	write, err := s.Documents.LatestWrite(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("LatestWrite: %v", err)
	}
	if write.Source != document.SourceWriteDocument || write.ContentSHA256 != document.ContentSHA256("a draft") {
		t.Errorf("recorded write = %+v, want the draft, as %v", write, document.SourceWriteDocument)
	}
}

// notLive is each way an application ID can have no live application,
// for the tests that its documents are refused.
var notLive = []struct {
	name string
	// applicationID is the application to use, given the one set up.
	applicationID func(t *testing.T, s testutil.TestApp, app application.Application) int64
	// refused checks the error.
	refused func(t *testing.T, err error, app application.Application)
}{
	{
		name:          "never handed out",
		applicationID: func(_ *testing.T, _ testutil.TestApp, app application.Application) int64 { return app.ID + 1 },
		refused: func(t *testing.T, err error, _ application.Application) {
			t.Helper()
			if !errors.Is(err, service.ErrApplicationNotFound) {
				t.Fatalf("err = %v, want service.ErrApplicationNotFound", err)
			}
		},
	},
	{
		name: "deleted",
		applicationID: func(t *testing.T, s testutil.TestApp, app application.Application) int64 {
			t.Helper()
			if _, err := s.Applications.Delete(context.Background(), app.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			return app.ID
		},
		refused: func(t *testing.T, err error, app application.Application) {
			t.Helper()
			var deleted *service.ApplicationDeletedError
			if !errors.As(err, &deleted) || deleted.PostingID != app.PostingID {
				t.Fatalf("err = %v, want an ApplicationDeletedError naming posting %d", err, app.PostingID)
			}
		},
	},
}

// Documents are kept by application ID alone, so a write for an ID with
// no live application would leave a draft for whichever gets that ID.
func TestDocumentService_Write_RefusesAnApplicationThatIsntLive(t *testing.T) {
	t.Parallel()
	for _, tt := range notLive {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			id := tt.applicationID(t, s, app)

			_, err := s.Documents.Write(ctx, id, document.CoverLetter, "a draft", service.NoDocumentYet, document.SourceWriteDocument)

			tt.refused(t, err, app)
			if _, err := s.Documents.LatestWrite(ctx, id, document.CoverLetter); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("LatestWrite = %v, want store.ErrNotFound: nothing recorded", err)
			}
			path, err := s.Docs.Path(id, document.CoverLetter)
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Stat(%s) = %v, want no document written", path, err)
			}
		})
	}
}

// A read is refused the same way, even of a document written before the
// application was deleted.
func TestDocumentService_Read_RefusesAnApplicationThatIsntLive(t *testing.T) {
	t.Parallel()
	for _, tt := range notLive {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			if _, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "a draft", service.NoDocumentYet, document.SourceWriteDocument); err != nil {
				t.Fatalf("Write: %v", err)
			}
			id := tt.applicationID(t, s, app)

			_, _, err := s.Documents.Read(ctx, id, document.CoverLetter)

			tt.refused(t, err, app)
		})
	}
}

// A write with replacesVersion goes ahead only while the document is that
// version, so a draft can't silently replace one written meanwhile.
func TestDocumentService_Write_ReplacesVersion(t *testing.T) {
	t.Parallel()
	const existing = "the draft on disk"
	tests := []struct {
		name string
		// onDisk is the document before the write; "" is none.
		onDisk          string
		replacesVersion string
		wantErr         error
	}{
		{name: "the version read", onDisk: existing, replacesVersion: document.ContentSHA256(existing)},
		{name: "another version", onDisk: existing, replacesVersion: document.ContentSHA256("what was read"), wantErr: documents.ErrChanged},
		{name: "none read, none there", onDisk: "", replacesVersion: service.NoDocumentYet},
		{name: "none read, one there", onDisk: existing, replacesVersion: service.NoDocumentYet, wantErr: documents.ErrChanged},
		{name: "no version given", onDisk: existing, replacesVersion: "", wantErr: service.ErrNoVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
			if tt.onDisk != "" {
				if _, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, tt.onDisk, service.NoDocumentYet, document.SourceEditor); err != nil {
					t.Fatalf("Write (existing): %v", err)
				}
			}

			_, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "a revision", tt.replacesVersion, document.SourceWriteDocument)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Write = %v, want %v", err, tt.wantErr)
			}
			want := "a revision"
			if tt.wantErr != nil {
				want = tt.onDisk
			}
			path, err := s.Docs.Path(app.ID, document.CoverLetter)
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if string(got) != want {
				t.Errorf("document after Write = %q, want %q", got, want)
			}
		})
	}
}

func TestDocumentService_Read_ReturnsTheDocumentAndItsPath(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	ctx := context.Background()
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)
	written, err := s.Documents.Write(ctx, app.ID, document.CoverLetter, "a draft", service.NoDocumentYet, document.SourceWriteDocument)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	path, content, err := s.Documents.Read(ctx, app.ID, document.CoverLetter)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if path != written || content != "a draft" {
		t.Errorf("Read = %q, %q; want %q, %q", path, content, written, "a draft")
	}
}

func TestDocumentService_Read_NotWrittenYet(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	app := testutil.MustApplyTo(t, s, testutil.MustIngestPosting(t, s, "job-1", "Software Engineer").ID)

	_, _, err := s.Documents.Read(context.Background(), app.ID, document.CoverLetter)

	if !errors.Is(err, service.ErrDocumentNotWritten) {
		t.Errorf("Read = %v, want service.ErrDocumentNotWritten", err)
	}
}
