package documents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustDoc is status.Doc(documentType), failing the test on an error.
func mustDoc(t *testing.T, status Status, documentType Type) Doc {
	t.Helper()
	doc, err := status.Doc(documentType)
	if err != nil {
		t.Fatalf("Doc(%s): %v", documentType, err)
	}
	return doc
}

func TestStore_Path_IsTheTypeNameUnderTheApplicationDir(t *testing.T) {
	t.Parallel()

	s := NewStore("/base")
	for _, documentType := range Types() {
		got, err := s.Path(42, documentType)
		if err != nil {
			t.Fatalf("Path(%s): %v", documentType, err)
		}
		if want := "/base/42/" + documentType.String() + ".md"; got != want {
			t.Errorf("Path(%s) = %q, want %q", documentType, got, want)
		}
	}
}

func TestStore_Status_FalseWhenFileAbsent(t *testing.T) {
	t.Parallel()

	status := NewStore(t.TempDir()).Status(1)
	for _, documentType := range Types() {
		if mustDoc(t, status, documentType).Exists {
			t.Errorf("%s: Exists = true, want false (file was never written)", documentType)
		}
	}
}

func TestStore_EnsureDir_CreatesDirWhenAbsent(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	status, err := s.EnsureDir(3)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	for _, documentType := range Types() {
		want, err := s.Path(3, documentType)
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		if got := mustDoc(t, status, documentType).Path; got != want {
			t.Errorf("%s: path = %q, want %q", documentType, got, want)
		}
	}
	info, err := os.Stat(filepath.Dir(mustDoc(t, status, CoverLetter).Path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected the application's document directory to exist as a directory")
	}
}

func TestStore_EnsureDir_IdempotentWhenDirAlreadyExists(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	if _, err := s.EnsureDir(5); err != nil {
		t.Fatalf("first EnsureDir: %v", err)
	}
	if _, err := s.EnsureDir(5); err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
}

func TestStore_Status_TrueWhenFilePresent(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	if _, err := s.EnsureDir(7); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, documentType := range Types() {
		p, err := s.Path(7, documentType)
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		if err := os.WriteFile(p, []byte("# "+documentType.String()), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", documentType, err)
		}
	}

	status := s.Status(7)
	for _, documentType := range Types() {
		if !mustDoc(t, status, documentType).Exists {
			t.Errorf("%s: Exists = false, want true (file was written)", documentType)
		}
	}
}

func TestStore_CanonicalResumePath_IsUnderTheCanonicalDir(t *testing.T) {
	t.Parallel()

	if got, want := NewStore("/base").CanonicalResumePath(), "/base/canonical/resume.md"; got != want {
		t.Errorf("CanonicalResumePath() = %q, want %q", got, want)
	}
}

func TestStore_ProfilePath_IsUnderTheCanonicalDir(t *testing.T) {
	t.Parallel()

	if got, want := NewStore("/base").ProfilePath(), "/base/canonical/profile.md"; got != want {
		t.Errorf("ProfilePath() = %q, want %q", got, want)
	}
}

func TestStore_Write_CreatesTheDocumentAndItsDir(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	for _, documentType := range Types() {
		got, err := s.Write(5, documentType, "# Draft\n")
		if err != nil {
			t.Fatalf("Write(%s): %v", documentType, err)
		}
		want, err := s.Path(5, documentType)
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		if got != want {
			t.Errorf("Write(%s) path = %q, want %q", documentType, got, want)
		}
		content, err := os.ReadFile(want)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(content) != "# Draft\n" {
			t.Errorf("%s content = %q, want %q", documentType, content, "# Draft\n")
		}
		info, err := os.Stat(want)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o644 {
			t.Errorf("%s mode = %v, want 0644 (what os.WriteFile gave it before)", documentType, mode)
		}
	}
}

// A reader in another process (the TUI while mcp-serve writes) must see
// the old document or the new one, never an empty or partial file (RFC
// 0007, H3).
func TestStore_Write_ReaderNeverSeesAPartialDocument(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	versions := []string{strings.Repeat("a", 1<<20), strings.Repeat("b", 1<<20)}
	p, err := s.Write(1, CoverLetter, versions[0])
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	done := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		defer close(done)
		for i := range 200 {
			if _, err := s.Write(1, CoverLetter, versions[i%2]); err != nil {
				writeErr <- err
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			select {
			case err := <-writeErr:
				t.Fatalf("Write: %v", err)
			default:
			}
			return
		default:
		}
		content, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if got := string(content); got != versions[0] && got != versions[1] {
			t.Fatalf("read %d bytes that are neither version: a reader saw a partial document", len(content))
		}
	}
}

func TestStore_Write_FailedWriteLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	p, err := s.Path(1, CoverLetter)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// A non-empty directory where the document belongs makes the rename
	// fail, after the temporary file has been written.
	if err := os.MkdirAll(filepath.Join(p, "blocker"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if _, err := s.Write(1, CoverLetter, "# Draft\n"); err == nil {
		t.Fatal("Write succeeded, want an error (a directory is in the document's place)")
	}

	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(p) {
			t.Errorf("left %q behind after a failed write", e.Name())
		}
	}
}
