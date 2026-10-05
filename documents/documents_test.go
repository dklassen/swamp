package documents

import (
	"os"
	"path/filepath"
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
