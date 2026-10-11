package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestApplicationNotesModel_New_SeedsTextareaWithNotes(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "existing notes", 80, 10, 0)
	if got := m.textarea.Value(); got != "existing notes" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "existing notes")
	}
	if !m.textarea.Focused() {
		t.Fatal("textarea should be focused on construction")
	}
}

func TestApplicationNotesModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "", 80, 10, 0)
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

	m := newApplicationNotesModel(nil, 7, "notes", 80, 10, 0)
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

	m := newApplicationNotesModel(nil, 7, "", 80, 10, 0)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if got := m.textarea.Value(); got != "hi" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "hi")
	}
}

func TestApplicationNotesModel_EscWithUnsavedEdits_AsksBeforeDiscarding(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "notes", 80, 10, 0)
	m.textarea.InsertString(" and more")
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || intent != nil {
		t.Fatalf("esc with unsaved edits: cmd %v, intent %T; want neither (screen stays to ask)", cmd, intent)
	}
	if view := m.View(); !strings.Contains(view, "Discard unsaved changes?") {
		t.Errorf("view doesn't ask before discarding:\n%s", view)
	}
}

func TestApplicationNotesModel_DiscardPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        tea.KeyMsg
		wantCancel bool
		wantSave   bool
		wantPrompt bool
	}{
		{name: "y discards", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}, wantCancel: true},
		{name: "n keeps editing", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}},
		{name: "esc keeps editing", key: tea.KeyMsg{Type: tea.KeyEsc}},
		{name: "ctrl+s saves", key: tea.KeyMsg{Type: tea.KeyCtrlS}, wantSave: true},
		{name: "other keys are ignored", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, wantPrompt: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newApplicationNotesModel(nil, 7, "notes", 80, 10, 0)
			m.textarea.InsertString(" and more")
			typed := m.textarea.Value()
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})

			cmd, intent := m.Update(tt.key)
			if _, cancelled := intent.(cancelApplicationNotesMsg); cancelled != tt.wantCancel {
				t.Errorf("intent = %T, want cancel: %v", intent, tt.wantCancel)
			}
			if saved := cmd != nil; saved != tt.wantSave {
				t.Errorf("save cmd returned: %v, want %v", saved, tt.wantSave)
			}
			if prompt := strings.Contains(m.View(), "Discard unsaved changes?"); !tt.wantCancel && prompt != tt.wantPrompt {
				t.Errorf("prompt shown: %v, want %v", prompt, tt.wantPrompt)
			}
			if got := m.textarea.Value(); got != typed {
				t.Errorf("textarea = %q, want %q unchanged", got, typed)
			}
		})
	}
}

// TestApplicationNotesModel_WhileSaving_IgnoresKeys: once ctrl+s is
// pressed, the screen waits for the save's result, so esc can't leave
// with the save's outcome unseen and typing can't change what's saved.
func TestApplicationNotesModel_WhileSaving_IgnoresKeys(t *testing.T) {
	t.Parallel()

	m := newApplicationNotesModel(nil, 7, "notes", 80, 10, 0)
	m.textarea.InsertString(" and more")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("x")}, {Type: tea.KeyCtrlS}} {
		if cmd, intent := m.Update(key); cmd != nil || intent != nil {
			t.Errorf("%v while saving: cmd %v, intent %T; want neither", key, cmd, intent)
		}
	}
	if got := m.textarea.Value(); got != "notes and more" {
		t.Errorf("textarea = %q, want %q", got, "notes and more")
	}
}
