package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func TestDocumentReviewSelectModel_New_SeedsOptionsFromDocumentStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docs := documents.NewStore(dir)
	if _, err := docs.EnsureDir(1); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1", "cover_letter.md"), []byte("Dear hiring manager"), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	m := newDocumentReviewSelectModel(docs, 1)

	if len(m.options) != 2 {
		t.Fatalf("len(options) = %d, want 2", len(m.options))
	}
	if m.options[0].documentType != store.DocumentTypeCoverLetter || !m.options[0].exists {
		t.Fatalf("options[0] = %+v, want cover_letter, exists=true", m.options[0])
	}
	if m.options[1].documentType != store.DocumentTypeResume || m.options[1].exists {
		t.Fatalf("options[1] = %+v, want resume, exists=false", m.options[1])
	}
}

func TestDocumentReviewSelectModel_CursorMovement_ClampsToOptions(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), 1)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestDocumentReviewSelectModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), 1)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelDocumentReviewSelectMsg); !ok {
		t.Fatalf("intent = %T, want cancelDocumentReviewSelectMsg", intent)
	}
}

func TestDocumentReviewSelectModel_Enter_OnMissingDocument_NoOp(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewSelectModel(documents.NewStore(t.TempDir()), 1)
	// cursor starts at 0 (Cover Letter), which doesn't exist in a fresh temp dir.
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (document doesn't exist)", cmd, intent)
	}
}

func TestDocumentReviewSelectModel_Enter_OnExistingDocument_ReturnsContentReadFromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docs := documents.NewStore(dir)
	if _, err := docs.EnsureDir(1); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	want := "Dear hiring manager, I am excited to apply."
	if err := os.WriteFile(filepath.Join(dir, "1", "cover_letter.md"), []byte(want), 0o644); err != nil {
		t.Fatalf("write cover letter: %v", err)
	}

	m := newDocumentReviewSelectModel(docs, 1)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterDocumentReviewFormMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterDocumentReviewFormMsg", intent)
	}
	if got.err != nil {
		t.Fatalf("err = %v, want nil", got.err)
	}
	if got.applicationID != 1 {
		t.Fatalf("applicationID = %d, want 1", got.applicationID)
	}
	if got.documentType != store.DocumentTypeCoverLetter {
		t.Fatalf("documentType = %q, want %q", got.documentType, store.DocumentTypeCoverLetter)
	}
	if got.content != want {
		t.Fatalf("content = %q, want %q", got.content, want)
	}
}
