package tui

import (
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
