package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// testExportApplication is the application every export-screen test
// drafts documents for.
func testExportApplication() store.ApplicationView {
	return store.ApplicationView{
		Application: store.Application{ID: 42, Status: store.ApplicationStatusStarted},
		Posting:     store.Posting{ID: 7, IngestedFields: store.IngestedFields{Title: "Delivery Platform"}},
		CompanyName: "WealthSimple",
	}
}

// writeApplicationDocuments creates a documents base dir containing the
// named markdown documents for applicationID, and returns a Store over
// it. Document types not named are left absent on disk.
func writeApplicationDocuments(t *testing.T, applicationID int64, present ...store.DocumentType) *documents.Store {
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

	docs := writeApplicationDocuments(t, 42, store.DocumentTypeCoverLetter)
	m := newApplicationExportModel(docs, testExportApplication(), "/home/dana/Desktop", 80)

	if got := m.textinput.Value(); got != "/home/dana/Desktop" {
		t.Errorf("destination prefill = %q, want %q", got, "/home/dana/Desktop")
	}
	if view := m.View(); !strings.Contains(view, "WealthSimple") || !strings.Contains(view, "Delivery Platform") {
		t.Errorf("View() = %q, want it to name the application being exported", view)
	}
}

func TestApplicationExportModel_EscCancels(t *testing.T) {
	t.Parallel()

	docs := writeApplicationDocuments(t, 42, store.DocumentTypeCoverLetter)
	m := newApplicationExportModel(docs, testExportApplication(), "/tmp", 80)

	_, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if _, ok := intent.(cancelApplicationExportMsg); !ok {
		t.Fatalf("intent = %T, want cancelApplicationExportMsg", intent)
	}
}

func TestApplicationExportModel_EnterExportsBothDocumentsWithDescriptiveNames(t *testing.T) {
	t.Parallel()

	docs := writeApplicationDocuments(t, 42, store.DocumentTypeCoverLetter, store.DocumentTypeResume)
	dest := t.TempDir()
	m := newApplicationExportModel(docs, testExportApplication(), dest, 80)

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
		filepath.Join(dest, "wealthsimple-delivery-platform-cover_letter.pdf"),
		filepath.Join(dest, "wealthsimple-delivery-platform-resume.pdf"),
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
	docs := writeApplicationDocuments(t, 42, store.DocumentTypeResume)
	dest := t.TempDir()
	m := newApplicationExportModel(docs, testExportApplication(), dest, 80)

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(applicationExportedMsg)

	if got.err != nil {
		t.Fatalf("export err = %v, want nil (a missing draft is skipped, not an error)", got.err)
	}
	wantPaths := []string{filepath.Join(dest, "wealthsimple-delivery-platform-resume.pdf")}
	if diff := cmp.Diff(wantPaths, got.paths); diff != "" {
		t.Errorf("exported paths mismatch (-want +got):\n%s", diff)
	}
	wantSkipped := []store.DocumentType{store.DocumentTypeCoverLetter}
	if diff := cmp.Diff(wantSkipped, got.skipped); diff != "" {
		t.Errorf("skipped mismatch (-want +got):\n%s", diff)
	}
}

func TestApplicationExportModel_ExpandsTildeInDestination(t *testing.T) {
	// Not parallel: t.Setenv can't be used with t.Parallel.
	home := t.TempDir()
	t.Setenv("HOME", home)

	docs := writeApplicationDocuments(t, 42, store.DocumentTypeResume)
	m := newApplicationExportModel(docs, testExportApplication(), "~/Desktop", 80)

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(applicationExportedMsg)

	if got.err != nil {
		t.Fatalf("export err = %v, want nil", got.err)
	}
	wantDir := filepath.Join(home, "Desktop")
	if got.dir != wantDir {
		t.Errorf("dir = %q, want %q (tilde expanded)", got.dir, wantDir)
	}
	wantPath := filepath.Join(wantDir, "wealthsimple-delivery-platform-resume.pdf")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("stat exported pdf: %v (want it written under the expanded home dir)", err)
	}
}
