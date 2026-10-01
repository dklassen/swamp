package documents

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// record stands in for store.DocumentReview or store.DocumentExport:
// anything recorded against a document's content at the time.
type record struct{ content string }

func recordIsCurrent(r record, content string) bool { return r.content == content }

func TestCurrent_KeepsOnlyRecordsMatchingTheDocumentOnDisk(t *testing.T) {
	t.Parallel()
	docs := NewStore(t.TempDir())
	if _, err := docs.EnsureDir(7); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	coverLetter, err := docs.Path(7, CoverLetter)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(coverLetter, []byte("v2"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// The resume was never written, so a record of it can't be current.
	records := map[Type]record{CoverLetter: {content: "v2"}, Resume: {content: "v1"}}

	got, err := Current(docs.Status(7), records, recordIsCurrent)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if diff := cmp.Diff(map[Type]record{CoverLetter: {content: "v2"}}, got, cmp.AllowUnexported(record{})); diff != "" {
		t.Errorf("Current mismatch (-want +got):\n%s", diff)
	}

	if err := os.WriteFile(coverLetter, []byte("v3"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err = Current(docs.Status(7), records, recordIsCurrent)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Current after the document changed = %v, want nothing current", got)
	}
}

// A read failure is not "the document changed": treating it that way
// would quietly hide a flagged review, so it's an error.
func TestCurrent_ReadFailureIsAnError(t *testing.T) {
	t.Parallel()
	docs := NewStore(t.TempDir())
	coverLetter, err := docs.Path(7, CoverLetter)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// A directory where the file should be: it exists, but can't be read.
	if err := os.MkdirAll(coverLetter, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if _, err := Current(docs.Status(7), map[Type]record{CoverLetter: {content: "v1"}}, recordIsCurrent); err == nil {
		t.Error("Current succeeded, want the read error")
	}
}
