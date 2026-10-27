package tui

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
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

func TestCompanyEditModel_Enter_EmptyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("")

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("cmd = %v, want nil (empty name)", cmd)
	}
}

func TestCompanyEditModel_Enter_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}
}

func TestCompanyEditModel_TypingKey_UpdatesInputValue(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.keyPressed(runeKey([]rune(" Corp")...))
	if got := m.nameInput.Value(); got != "Acme Corp" {
		t.Fatalf("nameInput.Value() = %q, want %q", got, "Acme Corp")
	}
}

func TestCompanyEditModel_Enter_WhitespaceOnlyName_NoOp(t *testing.T) {
	t.Parallel()

	m := newCompanyEditModel(nil, 7, "Acme")
	m.nameInput.SetValue("   ")

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("cmd = %v, want nil (whitespace-only name)", cmd)
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

	cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("cmd = nil, want a command that saves the company name")
	}

	if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}); cmd != nil {
		t.Fatalf("cmd = %v, want nil (esc while save pending)", cmd)
	}
}

// The edit takes keys only while the last focus signal named it.
func TestCompanyEditModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		focused   func(m *companyEditModel) message.ScreenInstance
		wantClose bool
	}{
		{name: "the edit in focus", focused: func(m *companyEditModel) message.ScreenInstance { return m.Instance() }, wantClose: true},
		{name: "another component in focus", focused: func(*companyEditModel) message.ScreenInstance { return newCompanyListModel(nil, nil).Instance() }, wantClose: false},
		{name: "nothing in focus", focused: func(*companyEditModel) message.ScreenInstance { return 0 }, wantClose: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyEditModel(nil, 7, "Acme")
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(focusOn(tt.focused(m)))
			cmd := r.Dispatch(tea.KeyPressMsg{Code: tea.KeyEsc})

			if closed := slices.Contains(runCmd(t, cmd), tea.Msg(message.CloseCompanyEditMsg{})); closed != tt.wantClose {
				t.Errorf("esc asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
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
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("enter: cmd = nil, want the save")
			}

			var got tea.Msg
			if cmd := m.companyNameUpdated(tt.updated(m)); cmd != nil {
				got = cmd()
			}

			if closed := got == (message.CloseCompanyEditMsg{}); closed != tt.wantClose {
				t.Errorf("companyNameUpdated sent %#v, want close = %v", got, tt.wantClose)
			}
			if blocked := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}) == nil; blocked != tt.wantSaving {
				t.Errorf("esc blocked = %v, want %v", blocked, tt.wantSaving)
			}
		})
	}
}
