package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCompanyEditModel_New_SeedsInputWithName(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	if got := m.nameInput.Value(); got != "Acme" {
		t.Fatalf("nameInput.Value() = %q, want %q", got, "Acme")
	}
	if !m.nameInput.Focused() {
		t.Fatal("nameInput should be focused on construction")
	}
}

func TestCompanyEditModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelCompanyEditMsg); !ok {
		t.Fatalf("intent = %T, want cancelCompanyEditMsg", intent)
	}
}

func TestCompanyEditModel_Enter_EmptyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("")

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (empty name)", cmd, intent)
	}
}

func TestCompanyEditModel_Enter_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestCompanyEditModel_TypingKey_UpdatesInputValue(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" Corp")})
	if got := m.nameInput.Value(); got != "Acme Corp" {
		t.Fatalf("nameInput.Value() = %q, want %q", got, "Acme Corp")
	}
}

func TestCompanyEditModel_Enter_WhitespaceOnlyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("   ")

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (whitespace-only name)", cmd, intent)
	}
}

func TestCompanyEditModel_Enter_TrimsSavedName(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created, err := s.CreateCompany(t.Context(), "Old Name", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	m := newCompanyEditModel(s, created.ID, created.Name)
	m.nameInput.SetValue("  Acme Corp  ")

	cmd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}
	msg := cmd()
	updated, ok := msg.(companyNameUpdatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want companyNameUpdatedMsg", msg)
	}
	if updated.err != nil {
		t.Fatalf("updateCompanyName error = %v", updated.err)
	}
	if updated.company.Name != "Acme Corp" {
		t.Fatalf("company.Name = %q, want %q (trimmed)", updated.company.Name, "Acme Corp")
	}
}
