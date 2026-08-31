package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// companyEditModel drives the edit-company screen: name only. source/
// source_ref are locked after creation (see decisions.log) and have no
// field here at all -- not just disabled, since there's nothing for this
// screen to do with them.
type companyEditModel struct {
	store     *store.Store
	companyID int64
	nameInput textinput.Model
}

// newCompanyEditModel returns an edit screen for companyID, seeded with
// its current name.
func newCompanyEditModel(s *store.Store, companyID int64, name string) companyEditModel {
	input := textinput.New()
	input.SetValue(name)
	input.Focus()
	return companyEditModel{store: s, companyID: companyID, nameInput: input}
}

// cancelCompanyEditMsg signals that App should switch back to the
// company-list screen without saving.
type cancelCompanyEditMsg struct{}

func (m *companyEditModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyEsc:
		return nil, cancelCompanyEditMsg{}
	case tea.KeyEnter:
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			return nil, nil
		}
		return updateCompanyName(m.store, m.companyID, name), nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd, nil
}

func (m *companyEditModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Edit company") + "\n")
	b.WriteString(focusedLabel.Render("Name:") + " " + m.nameInput.View() + "\n")
	b.WriteString(helpStyle.Render("enter: save  esc: cancel"))
	return b.String()
}
