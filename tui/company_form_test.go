package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCompanyFormModel_New_FocusesFirstField(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	if m.focus != formFieldName {
		t.Fatalf("focus = %d, want formFieldName", m.focus)
	}
	if !m.inputs[formFieldName].Focused() {
		t.Fatal("first field should be focused on construction")
	}
}

func TestCompanyFormModel_Tab_AdvancesFocus(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyTab})

	if m.focus != formFieldSourceRef {
		t.Fatalf("focus after tab = %d, want formFieldSourceRef", m.focus)
	}
	if !m.inputs[formFieldSourceRef].Focused() {
		t.Fatal("sourceRef field should be focused after tab")
	}
	if m.inputs[formFieldName].Focused() {
		t.Fatal("name field should be blurred after tab")
	}
}

func TestCompanyFormModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelCompanyFormMsg); !ok {
		t.Fatalf("intent = %T, want cancelCompanyFormMsg", intent)
	}
}

func TestCompanyFormModel_Enter_EmptyFields_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (empty fields)", cmd, intent)
	}
}

func TestCompanyFormModel_Enter_ValidFields_ReturnsCreateCompanyCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyFormModel(nil)
	m.inputs[formFieldName].SetValue("Acme")
	m.inputs[formFieldSourceRef].SetValue("acme")

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the company")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}
