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

func TestExists_FalseWhenFileAbsent(t *testing.T) {
	t.Parallel()

	paths := ForApplication(t.TempDir(), 1)

	if paths.CoverLetterExists() {
		t.Error("CoverLetterExists() = true, want false (file was never written)")
	}
	if paths.ResumeExists() {
		t.Error("ResumeExists() = true, want false (file was never written)")
	}
}

func TestExists_TrueWhenFilePresent(t *testing.T) {
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

	if !paths.CoverLetterExists() {
		t.Error("CoverLetterExists() = false, want true (file was written)")
	}
	if !paths.ResumeExists() {
		t.Error("ResumeExists() = false, want true (file was written)")
	}
}
