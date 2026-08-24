package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestApplicationNotesModel_New_SeedsTextareaWithNotes(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "existing notes", 80, 10)
	if got := m.textarea.Value(); got != "existing notes" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "existing notes")
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestApplicationNotesModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "", 80, 10)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelApplicationNotesMsg); !ok {
		t.Fatalf("intent = %T, want cancelApplicationNotesMsg", intent)
	}
}

func TestApplicationNotesModel_CtrlS_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "notes", 80, 10)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves application notes")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestApplicationNotesModel_TypingKey_UpdatesTextareaValue(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "", 80, 10)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if got := m.textarea.Value(); got != "hi" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "hi")
	}
}
