package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/tui/message"
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
			if got := cmd(); got != (message.CloseCompanyDeleteMsg{}) {
				t.Errorf("keyPressed(%s) sent %#v, want message.CloseCompanyDeleteMsg{}", tt.name, got)
			}
		})
	}
}

// A late result from an earlier confirmation leaves this one up.
func TestCompanyDeleteModel_CompanyDeleted_AsksToCloseForItsCompanyOnly(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		deleted   message.CompanyDeletedMsg
		wantClose bool
	}{
		{"its company", message.CompanyDeletedMsg{CompanyID: 1}, true},
		{"its company, failed", message.CompanyDeletedMsg{CompanyID: 1, Err: errors.New("disk full")}, true},
		{"another company", message.CompanyDeletedMsg{CompanyID: 2}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newCompanyDeleteModel(nil, company.Company{ID: 1, Name: "Acme"}, 0, 80)

			var got tea.Msg
			if cmd := m.companyDeleted(tt.deleted); cmd != nil {
				got = cmd()
			}
			if closed := got == (message.CloseCompanyDeleteMsg{}); closed != tt.wantClose {
				t.Errorf("companyDeleted(%+v) sent %#v, want close = %v", tt.deleted, got, tt.wantClose)
			}
		})
	}
}
