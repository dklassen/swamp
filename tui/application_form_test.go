package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy: a line the
// form can't read isn't saved; the screen stays open and names the line,
// so the user can fix it without losing what they typed (#184).
func TestApplicationFormModel_InvalidText_StaysOpenAndSaysWhy(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	m.textarea.SetValue("resume: maybe\nWhy us? *\n")
	cmd, intent := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
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
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil || intent != nil {
		t.Fatalf("esc with unsaved edits: cmd %v, intent %T; want neither (screen stays to ask)", cmd, intent)
	}
	if !m.confirmingDiscard {
		t.Error("esc with unsaved edits didn't open the discard question")
	}
}

// TestApplicationFormModel_DiscardPrompt covers each answer to the prompt
// esc raises over unsaved edits.
func TestApplicationFormModel_DiscardPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        tea.KeyPressMsg
		wantCancel bool
		wantSave   bool
		wantPrompt bool
	}{
		{name: "y discards", key: runeKey('y'), wantCancel: true},
		{name: "n keeps editing", key: runeKey('n')},
		{name: "esc keeps editing", key: tea.KeyPressMsg{Code: tea.KeyEsc}},
		{name: "ctrl+s saves", key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, wantSave: true},
		{name: "other keys are ignored", key: runeKey('x'), wantPrompt: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newApplicationFormModel(nil, 7, nil, 80, 20)
			m.textarea.InsertString("Why us? *\n")
			typed := m.textarea.Value()
			m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

			cmd, intent := m.Update(tt.key)
			if _, cancelled := intent.(cancelApplicationFormMsg); cancelled != tt.wantCancel {
				t.Errorf("intent = %T, want cancel: %v", intent, tt.wantCancel)
			}
			if saved := cmd != nil; saved != tt.wantSave {
				t.Errorf("save cmd returned: %v, want %v", saved, tt.wantSave)
			}
			if prompt := m.confirmingDiscard; !tt.wantCancel && prompt != tt.wantPrompt {
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
	if _, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}); intent != (cancelApplicationFormMsg{}) {
		t.Fatalf("intent = %T, want cancelApplicationFormMsg", intent)
	}
}

// TestApplicationFormModel_WhileSaving_IgnoresKeys: once a valid form is
// sent to be saved, the screen waits for the result.
func TestApplicationFormModel_WhileSaving_IgnoresKeys(t *testing.T) {
	t.Parallel()

	m := newApplicationFormModel(nil, 7, nil, 80, 20)
	m.textarea.InsertString("Why us? *\n")
	if cmd, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+s on a valid form returned no save cmd")
	}
	typed := m.textarea.Value()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('x'), {Code: 's', Mod: tea.ModCtrl}} {
		if cmd, intent := m.Update(key); cmd != nil || intent != nil {
			t.Errorf("%v while saving: cmd %v, intent %T; want neither", key, cmd, intent)
		}
	}
	if got := m.textarea.Value(); got != typed {
		t.Errorf("textarea = %q, want %q", got, typed)
	}
}
