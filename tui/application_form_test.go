package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy: a line the
// form can't read isn't saved; the screen stays open and names the line,
// so the user can fix it without losing what they typed (#184).
func TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	m.textarea.SetValue("resume: maybe\nWhy us? *\n")
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil || intent != nil {
		t.Fatalf("ctrl+s on invalid text: cmd %v, intent %T; want neither (nothing saved, screen stays)", cmd, intent)
	}
	if view := m.View(); !strings.Contains(view, "line 1") || !strings.Contains(view, "maybe") {
		t.Errorf("view doesn't say which line is wrong:\n%s", view)
	}
	if got := m.textarea.Value(); !strings.Contains(got, "Why us? *") {
		t.Errorf("textarea lost what was typed: %q", got)
	}
}

// TestApplicationFormModel_EscWithUnsavedEdits_AsksBeforeDiscarding: esc
// doesn't throw away pasted questions without asking.
func TestApplicationFormModel_EscWithUnsavedEdits_AsksBeforeDiscarding(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	m.textarea.InsertString("Why us? *\n")
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || intent != nil {
		t.Fatalf("esc with unsaved edits: cmd %v, intent %T; want neither (screen stays to ask)", cmd, intent)
	}
	if view := m.View(); !strings.Contains(view, "Discard unsaved changes?") {
		t.Errorf("view doesn't ask before discarding:\n%s", view)
	}
}

// TestApplicationFormModel_DiscardPrompt covers each answer to the prompt
// esc raises over unsaved edits.
func TestApplicationFormModel_DiscardPrompt(t *testing.T) {
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

			m := newApplicationFormModel(nil, 7, nil, 80, 20)
			m.textarea.InsertString("Why us? *\n")
			typed := m.textarea.Value()
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})

			cmd, intent := m.Update(tt.key)
			if _, cancelled := intent.(cancelApplicationFormMsg); cancelled != tt.wantCancel {
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

func TestApplicationFormModel_EscWithoutEdits_Cancels(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	if _, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); intent != (cancelApplicationFormMsg{}) {
		t.Fatalf("intent = %T, want cancelApplicationFormMsg", intent)
	}
}
