package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func TestDocumentReviewFormModel_New_SeedsFocusedEmptyTextarea(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, 5, 1, store.DocumentTypeCoverLetter, "Dear hiring manager", 80, 10)
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("textarea.Value() = %q, want empty", got)
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestDocumentReviewFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, 5, 1, store.DocumentTypeCoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelDocumentReviewFormMsg); !ok {
		t.Fatalf("intent = %T, want cancelDocumentReviewFormMsg", intent)
	}
}

func TestDocumentReviewFormModel_CtrlP_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, 5, 1, store.DocumentTypeCoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the review as passed")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestDocumentReviewFormModel_CtrlF_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, 5, 1, store.DocumentTypeCoverLetter, "content", 80, 10)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the review as flagged")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestDocumentReviewFormModel_TypingKey_UpdatesTextareaValue(t *testing.T) {
	t.Parallel()

	m := newDocumentReviewFormModel(nil, 5, 1, store.DocumentTypeCoverLetter, "content", 80, 10)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("too generic")})
	if got := m.textarea.Value(); got != "too generic" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "too generic")
	}
}
