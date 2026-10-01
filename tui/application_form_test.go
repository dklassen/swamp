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
