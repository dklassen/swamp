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
	// saving is true from the moment Enter dispatches updateCompanyName
	// until saveResolved is called with its result. bubbletea has no way
	// to cancel an already-dispatched command, so Esc can't actually stop
	// an in-flight save -- it's blocked instead, so "esc: cancel" stays
	// true rather than racing the save (see decisions.log, #37).
	saving bool
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
		if m.saving {
			return nil, nil
		}
		return nil, cancelCompanyEditMsg{}
	case tea.KeyEnter:
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			return nil, nil
		}
		m.saving = true
		return updateCompanyName(m.store, m.companyID, name), nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd, nil
}

// saveResolved marks the in-flight save (if any) as done, letting Esc
// cancel normally again. Called by App once companyNameUpdatedMsg
// arrives, whether the save succeeded or failed -- either way, there's
// no longer a save in flight for Esc to race.
func (m *companyEditModel) saveResolved() {
	m.saving = false
}

func (m *companyEditModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Edit company") + "\n")
	b.WriteString(focusedLabel.Render("Name:") + " " + m.nameInput.View() + "\n")
	b.WriteString(helpStyle.Render("enter: save  esc: cancel"))
	return b.String()
}
