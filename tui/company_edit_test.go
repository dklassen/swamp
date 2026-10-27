package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
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
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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

	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (empty name)", cmd, intent)
	}
}

func TestCompanyEditModel_Enter_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	m.Update(runeKey([]rune(" Corp")...))
	if got := m.nameInput.Value(); got != "Acme Corp" {
		t.Fatalf("nameInput.Value() = %q, want %q", got, "Acme Corp")
	}
}

func TestCompanyEditModel_Enter_WhitespaceOnlyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("   ")

	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (whitespace-only name)", cmd, intent)
	}
}

func TestCompanyEditModel_Enter_TrimsSavedName(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	created, err := s.Companies.Add(t.Context(), "Old Name", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	m := newCompanyEditModel(s.Companies, created.ID, created.Name)
	m.nameInput.SetValue("  Acme Corp  ")

	cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}
	msg := cmd()
	updated, ok := msg.(message.CompanyNameUpdatedMsg)
	if !ok {
		t.Fatalf("msg = %T, want message.CompanyNameUpdatedMsg", msg)
	}
	if updated.Err != nil {
		t.Fatalf("updateCompanyName error = %v", updated.Err)
	}
	if updated.Company.Name != "Acme Corp" {
		t.Fatalf("company.Name = %q, want %q (trimmed)", updated.Company.Name, "Acme Corp")
	}
}

func TestCompanyEditModel_Esc_WhileSavePending_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("Acme Corp")

	// Enter dispatches the save command but doesn't run it -- bubbletea
	// runs commands async, so at this point the save is "in flight" from
	// the model's perspective even though nothing has executed yet.
	if cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}

	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (esc while save pending)", cmd, intent)
	}
}

// A result from an earlier opening of the edit leaves this one as it is:
// still saving, so esc stays blocked, and open.
func TestCompanyEditModel_CompanyNameUpdated_ActsOnItsOwnResultOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		updated    func(m *companyEditModel) message.CompanyNameUpdatedMsg
		wantClose  bool
		wantSaving bool
	}{
		{name: "its own, saved", updated: func(m *companyEditModel) message.CompanyNameUpdatedMsg {
			return message.CompanyNameUpdatedMsg{From: m.Instance()}
		}, wantClose: true},
		{name: "its own, failed", updated: func(m *companyEditModel) message.CompanyNameUpdatedMsg {
			return message.CompanyNameUpdatedMsg{From: m.Instance(), Err: errors.New("disk full")}
		}},
		{name: "an earlier edit's", updated: func(*companyEditModel) message.CompanyNameUpdatedMsg {
			return message.CompanyNameUpdatedMsg{From: newCompanyEditModel(nil, 7, "Acme").Instance()}
		}, wantSaving: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyEditModel(nil, 7, "Acme")
			if cmd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("enter: cmd = nil, want the save")
			}

			var got tea.Msg
			if cmd := m.companyNameUpdated(tt.updated(m)); cmd != nil {
				got = cmd()
			}

			if closed := got == (message.CloseCompanyEditMsg{}); closed != tt.wantClose {
				t.Errorf("companyNameUpdated sent %#v, want close = %v", got, tt.wantClose)
			}
			_, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			if blocked := intent == nil; blocked != tt.wantSaving {
				t.Errorf("esc blocked = %v, want %v", blocked, tt.wantSaving)
			}
		})
	}
}
