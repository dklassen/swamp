package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// Keeping is the default, so enter can't delete by habit.
func TestCompanyDeleteModel_KeepKeys_AskToClose(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"n", runeKey('n')},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEsc}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyDeleteModel(nil, company.Company{ID: 1, Name: "Acme"}, 0, 80)

			cmd := m.keyPressed(tt.key)
			if cmd == nil {
				t.Fatalf("keyPressed(%s) returned no command, want one asking to close", tt.name)
			}
			if got := cmd(); !pops(got, m.Instance()) {
				t.Errorf("keyPressed(%s) sent %#v, want a pop of the confirmation", tt.name, got)
			}
		})
	}
}

// The confirmation closes once its company is gone, whoever deleted it,
// and when its own delete fails. A late result from an earlier
// confirmation leaves this one up.
func TestCompanyDeleteModel_ClosesWhenItsCompanyIsDeletedOrItsDeleteFails(t *testing.T) {
	t.Parallel()

	acme := company.Company{ID: 1, Name: "Acme"}
	other := message.Source{Instance: message.NewScreenInstance()}
	failed := func(src message.Source) tea.Msg {
		return message.New(src, message.Subject{Type: message.TypeCompany, ID: acme.ID}, message.OpDeleted, time.Now().UTC(),
			model.ErrorOf(errors.New("disk full")))
	}
	for _, tt := range []struct {
		name      string
		event     func(m *companyDeleteModel) tea.Msg
		wantClose bool
	}{
		{name: "its company, deleted by it", event: func(m *companyDeleteModel) tea.Msg {
			return message.Deleted(message.Source{Instance: m.Instance()}, acme.ID, time.Now().UTC(), companySnapshot(acme))
		}, wantClose: true},
		{name: "its company, deleted elsewhere", event: func(*companyDeleteModel) tea.Msg {
			return message.Deleted(other, acme.ID, time.Now().UTC(), companySnapshot(acme))
		}, wantClose: true},
		{name: "its delete, failed", event: func(m *companyDeleteModel) tea.Msg {
			return failed(message.Source{Instance: m.Instance()})
		}, wantClose: true},
		{name: "another company, deleted", event: func(*companyDeleteModel) tea.Msg {
			return message.Deleted(other, 2, time.Now().UTC(), message.Company{ID: 2, Name: "Globex"})
		}},
		{name: "its company, renamed", event: func(*companyDeleteModel) tea.Msg {
			return message.Updated(other, acme.ID, time.Now().UTC(), companySnapshot(acme), message.Company{ID: acme.ID, Name: "Acme Corp"})
		}},
		{name: "an earlier confirmation's delete, failed", event: func(*companyDeleteModel) tea.Msg {
			return failed(other)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyDeleteModel(nil, acme, 0, 80)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			closed := popsIn(runCmd(t, r.Dispatch(tt.event(m))), m.Instance())

			if closed != tt.wantClose {
				t.Errorf("asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
	}
}

// The confirmation takes keys only while the last focus signal named it.
func TestCompanyDeleteModel_TakesKeysOnlyWhileFocused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		focused   func(m *companyDeleteModel) message.ScreenInstance
		wantClose bool
	}{
		{name: "the confirmation in focus", focused: func(m *companyDeleteModel) message.ScreenInstance { return m.Instance() }, wantClose: true},
		{name: "another component in focus", focused: func(*companyDeleteModel) message.ScreenInstance { return newCompanyListModel(nil, nil).Instance() }, wantClose: false},
		{name: "nothing in focus", focused: func(*companyDeleteModel) message.ScreenInstance { return 0 }, wantClose: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyDeleteModel(nil, company.Company{ID: 1, Name: "Acme"}, 0, 80)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(focusOn(tt.focused(m)))
			cmd := r.Dispatch(runeKey('n'))

			if closed := cmd != nil && pops(cmd(), m.Instance()); closed != tt.wantClose {
				t.Errorf("n asked to close = %v, want %v", closed, tt.wantClose)
			}
		})
	}
}

func TestCompanyDeleteModel_Y_SendsTheCompanyDeleted(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	m := newCompanyDeleteModel(s.Companies, acme, 0, 80)

	msg := m.keyPressed(runeKey('y'))()

	got, ok := msg.(message.Event[message.Changed[message.Company]])
	if !ok {
		t.Fatalf("y sent %T, want message.Event[message.Changed[message.Company]]", msg)
	}
	assertEventFrom(t, got.Meta, message.Source{Instance: m.Instance()})
	want := message.Deleted(got.Meta.Source, acme.ID, got.Meta.At, companySnapshot(acme))
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(message.Meta{}, "ID")); diff != "" {
		t.Errorf("y sent (-want +got):\n%s", diff)
	}
}
