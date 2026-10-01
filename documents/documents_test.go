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

// ByName finds a document by its type's name, which is its file's base
// name. Anything else is an error: falling back to one of the documents
// would silently use the wrong file (RFC 0004).
func TestStatus_ByName(t *testing.T) {
	t.Parallel()
	status := NewStore(t.TempDir()).Status(7)

	tests := []struct {
		name    string
		want    Doc
		wantErr bool
	}{
		{name: "cover_letter", want: status.CoverLetter},
		{name: "resume", want: status.Resume},
		{name: "answers", wantErr: true},
		{name: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := status.ByName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ByName(%q) error = %v, want error %v", tt.name, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ByName(%q) = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
}
