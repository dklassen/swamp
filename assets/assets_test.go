package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForApplication_ComputesExpectedPaths(t *testing.T) {
	t.Parallel()

	got := ForApplication("/base", 42)

	wantCoverLetter := "/base/42/cover_letter.md"
	wantResume := "/base/42/resume.md"
	if got.CoverLetter != wantCoverLetter {
		t.Errorf("CoverLetter = %q, want %q", got.CoverLetter, wantCoverLetter)
	}
	if got.Resume != wantResume {
		t.Errorf("Resume = %q, want %q", got.Resume, wantResume)
	}
}

func TestStore_Status_FalseWhenFileAbsent(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	status := s.Status(1)

	if status.CoverLetter.Exists {
		t.Error("CoverLetter.Exists = true, want false (file was never written)")
	}
	if status.Resume.Exists {
		t.Error("Resume.Exists = true, want false (file was never written)")
	}
}

func TestStore_EnsureDir_CreatesDirWhenAbsent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	paths, err := NewStore(base).EnsureDir(3)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	want := ForApplication(base, 3)
	if paths != want {
		t.Errorf("paths = %+v, want %+v", paths, want)
	}

	info, err := os.Stat(filepath.Dir(paths.CoverLetter))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected the application's document directory to exist as a directory")
	}
}

func TestStore_EnsureDir_IdempotentWhenDirAlreadyExists(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	s := NewStore(base)

	if _, err := s.EnsureDir(5); err != nil {
		t.Fatalf("first EnsureDir: %v", err)
	}
	paths, err := s.EnsureDir(5)
	if err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}

	want := ForApplication(base, 5)
	if paths != want {
		t.Errorf("paths = %+v, want %+v", paths, want)
	}
}

func TestStore_Status_TrueWhenFilePresent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	paths := ForApplication(base, 7)

	if err := os.MkdirAll(filepath.Dir(paths.CoverLetter), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(paths.CoverLetter, []byte("# Cover Letter"), 0o644); err != nil {
		t.Fatalf("WriteFile cover letter: %v", err)
	}
	if err := os.WriteFile(paths.Resume, []byte("# Resume"), 0o644); err != nil {
		t.Fatalf("WriteFile resume: %v", err)
	}

	status := NewStore(base).Status(7)

	if !status.CoverLetter.Exists {
		t.Error("CoverLetter.Exists = false, want true (file was written)")
	}
	if status.CoverLetter.Path != paths.CoverLetter {
		t.Errorf("CoverLetter.Path = %q, want %q", status.CoverLetter.Path, paths.CoverLetter)
	}
	if !status.Resume.Exists {
		t.Error("Resume.Exists = false, want true (file was written)")
	}
	if status.Resume.Path != paths.Resume {
		t.Errorf("Resume.Path = %q, want %q", status.Resume.Path, paths.Resume)
	}
}
