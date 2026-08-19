package documents

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
