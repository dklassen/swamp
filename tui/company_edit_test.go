package tui

import (
	"errors"
	"testing"
	"time"

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
	updated, ok := msg.(message.Event[message.Changed[message.Company]])
	if !ok {
		t.Fatalf("msg = %T, want message.Event[message.Changed[message.Company]]", msg)
	}
	assertEventFrom(t, updated.Meta, message.Source{Instance: m.Instance()})
	if updated.Meta.Op != message.OpUpdated || updated.Meta.Subject != (message.Subject{Type: message.TypeCompany, ID: created.ID}) {
		t.Errorf("Meta = %+v, want %v of company %d", updated.Meta, message.OpUpdated, created.ID)
	}
	if got, want := updated.Payload.Before, companySnapshot(*created); got != want {
		t.Errorf("Before = %+v, want %+v", got, want)
	}
	if got := updated.Payload.After.Name; got != "Acme Corp" {
		t.Errorf("After.Name = %q, want %q (trimmed)", got, "Acme Corp")
	}
	if !updated.Meta.At.Equal(updated.Payload.After.UpdatedAt) {
		t.Errorf("Meta.At = %v, want After.UpdatedAt %v", updated.Meta.At, updated.Payload.After.UpdatedAt)
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

			if closed := popsIn(runCmd(t, cmd), m.Instance()); closed != tt.wantClose {
				t.Errorf("esc asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
	}
}

// A result from an earlier opening of the edit leaves this one as it is:
// still saving, so esc stays blocked, and open.
func TestCompanyEditModel_ActsOnItsOwnResultOnly(t *testing.T) {
	t.Parallel()

	before := message.Company{ID: 7, Name: "Acme"}
	after := message.Company{ID: 7, Name: "Acme Corp"}
	renamed := func(instance message.ScreenInstance) tea.Msg {
		return message.Updated(message.Source{Instance: instance}, 7, time.Now().UTC(), before, after)
	}
	tests := []struct {
		name       string
		result     func(m *companyEditModel) tea.Msg
		wantClose  bool
		wantSaving bool
	}{
		{name: "its own, saved", result: func(m *companyEditModel) tea.Msg { return renamed(m.Instance()) }, wantClose: true},
		{name: "its own, failed", result: func(m *companyEditModel) tea.Msg {
			return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeCompany, ID: 7},
				message.OpUpdated, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
		}},
		{name: "an earlier edit's", result: func(*companyEditModel) tea.Msg {
			return renamed(newCompanyEditModel(nil, 7, "Acme").Instance())
		}, wantSaving: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyEditModel(nil, 7, "Acme")
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if cmd := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
				t.Fatal("enter: cmd = nil, want the save")
			}

			closed := popsIn(runCmd(t, r.Dispatch(tt.result(m))), m.Instance())

			if closed != tt.wantClose {
				t.Errorf("asked to close = %v, want %v", closed, tt.wantClose)
			}
			if blocked := m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEsc}) == nil; blocked != tt.wantSaving {
				t.Errorf("esc blocked = %v, want %v", blocked, tt.wantSaving)
			}
		})
	}
}
