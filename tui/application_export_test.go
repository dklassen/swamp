package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
)

// testExportApplication is the application every export-screen test
// drafts documents for.
func testExportApplication() posting.ActiveApplication {
	return posting.ActiveApplication{
		Application: application.Application{ID: 42, Status: application.Started},
		Posting:     posting.Posting{ID: 7, IngestedFields: posting.IngestedFields{Title: "Delivery Platform"}},
		CompanyName: "Northwind",
	}
}

// writeApplicationDocuments creates a documents base dir containing the
// named markdown documents for applicationID, and returns a Store over
// it. Document types not named are left absent on disk.
func writeApplicationDocuments(t *testing.T, applicationID int64, present ...document.Type) *documents.Store {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, strconv.FormatInt(applicationID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir documents dir: %v", err)
	}
	for _, documentType := range present {
		path := filepath.Join(dir, documentType.String()+".md")
		if err := os.WriteFile(path, []byte("# Heading\n\nBody text.\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return documents.NewStore(base)
}

func TestApplicationExportModel_PrefillsDestinationWithDefaultDir(t *testing.T) {
	t.Parallel()

	docs := writeApplicationDocuments(t, 42, document.CoverLetter)
	m := newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication(), "/home/dana/Desktop", 80)

	if got := m.textinput.Value(); got != "/home/dana/Desktop" {
		t.Errorf("destination prefill = %q, want %q", got, "/home/dana/Desktop")
	}
	if view := m.View(); !strings.Contains(view, "Northwind") || !strings.Contains(view, "Delivery Platform") {
		t.Errorf("View() = %q, want it to name the application being exported", view)
	}
}

func TestApplicationExportModel_EscCancels(t *testing.T) {
	t.Parallel()

	docs := writeApplicationDocuments(t, 42, document.CoverLetter)
	m := newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication(), "/tmp", 80)

	_, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if _, ok := intent.(cancelApplicationExportMsg); !ok {
		t.Fatalf("intent = %T, want cancelApplicationExportMsg", intent)
	}
}

func TestApplicationExportModel_EnterExportsBothDocumentsWithDescriptiveNames(t *testing.T) {
	t.Parallel()

	docs := writeApplicationDocuments(t, 42, document.CoverLetter, document.Resume)
	dest := t.TempDir()
	m := newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication(), dest, 80)

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent != nil {
		t.Fatalf("intent = %T, want nil (export runs as an async cmd)", intent)
	}
	if cmd == nil {
		t.Fatal("cmd = nil, want an export command")
	}

	got, ok := cmd().(applicationExportedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want applicationExportedMsg", cmd())
	}
	if got.err != nil {
		t.Fatalf("export err = %v, want nil", got.err)
	}

	want := []string{
		filepath.Join(dest, "northwind-delivery-platform-cover_letter.pdf"),
		filepath.Join(dest, "northwind-delivery-platform-resume.pdf"),
	}
	if diff := cmp.Diff(want, got.paths); diff != "" {
		t.Errorf("exported paths mismatch (-want +got):\n%s", diff)
	}
	for _, path := range want {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read exported pdf: %v", err)
		}
		if len(content) < 4 || string(content[:4]) != "%PDF" {
			t.Errorf("%s does not start with %%PDF", path)
		}
	}
}

func TestApplicationExportModel_SkipsUndraftedDocumentsWithoutFailing(t *testing.T) {
	t.Parallel()

	// Resume drafted, cover letter never written.
	docs := writeApplicationDocuments(t, 42, document.Resume)
	dest := t.TempDir()
	m := newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication(), dest, 80)

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(applicationExportedMsg)

	if got.err != nil {
		t.Fatalf("export err = %v, want nil (a missing draft is skipped, not an error)", got.err)
	}
	wantPaths := []string{filepath.Join(dest, "northwind-delivery-platform-resume.pdf")}
	if diff := cmp.Diff(wantPaths, got.paths); diff != "" {
		t.Errorf("exported paths mismatch (-want +got):\n%s", diff)
	}
	wantSkipped := []document.Type{document.CoverLetter}
	if diff := cmp.Diff(wantSkipped, got.skipped); diff != "" {
		t.Errorf("skipped mismatch (-want +got):\n%s", diff)
	}
}

func TestApplicationExportModel_ExpandsTildeInDestination(t *testing.T) {
	// Not parallel: t.Setenv can't be used with t.Parallel.
	home := t.TempDir()
	t.Setenv("HOME", home)

	docs := writeApplicationDocuments(t, 42, document.Resume)
	m := newApplicationExportModel(testutil.NewTestApp(t).Documents, docs, testExportApplication(), "~/Desktop", 80)

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(applicationExportedMsg)

	if got.err != nil {
		t.Fatalf("export err = %v, want nil", got.err)
	}
	wantDir := filepath.Join(home, "Desktop")
	if got.dir != wantDir {
		t.Errorf("dir = %q, want %q (tilde expanded)", got.dir, wantDir)
	}
	wantPath := filepath.Join(wantDir, "northwind-delivery-platform-resume.pdf")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("stat exported pdf: %v (want it written under the expanded home dir)", err)
	}
}

// Each exported document is recorded with the content it was exported
// from, which is what tells "passed, not exported" apart from
// "exported, ready to submit".
func TestApplicationExportModel_RecordsEachExport(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	docs := writeApplicationDocuments(t, 42, document.CoverLetter)
	dest := t.TempDir()
	m := newApplicationExportModel(s.Documents, docs, testExportApplication(), dest, 80)

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := cmd().(applicationExportedMsg); got.err != nil {
		t.Fatalf("export err = %v, want nil", got.err)
	}

	exports, err := s.Documents.LatestExports(context.Background(), 42)
	if err != nil {
		t.Fatalf("LatestExports: %v", err)
	}
	coverLetter, ok := exports[document.CoverLetter]
	if !ok {
		t.Fatal("no cover letter export recorded")
	}
	if want := filepath.Join(dest, "northwind-delivery-platform-cover_letter.pdf"); coverLetter.Path != want {
		t.Errorf("export path = %q, want %q", coverLetter.Path, want)
	}
	if !coverLetter.IsCurrent("# Heading\n\nBody text.\n") {
		t.Error("export isn't current for the content that was exported")
	}
	if _, ok := exports[document.Resume]; ok {
		t.Error("a resume export was recorded, but there was no resume to export")
	}
}
