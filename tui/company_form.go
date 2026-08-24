package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// formInputs indices: name, then source ref. Source is fixed to "ashby"
// for now (the only supported job board), so there's no source picker.
const (
	formFieldName = iota
	formFieldSourceRef
	formFieldCount
)

// companyFormModel drives the add-company screen. It holds the store it
// needs to dispatch createCompany, and the form's own private input/focus
// state -- no other screen reads or writes this state.
type companyFormModel struct {
	store  *store.Store
	inputs []textinput.Model
	focus  int
}

// newCompanyFormModel returns a fresh, blank form with the first field
// focused -- constructed anew each time the screen is entered, replacing
// the previous reset-in-place pattern (App.formInputs[i].SetValue("")).
func newCompanyFormModel(s *store.Store) companyFormModel {
	inputs := make([]textinput.Model, formFieldCount)
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[0].Focus()
	return companyFormModel{store: s, inputs: inputs}
}

// cancelCompanyFormMsg signals that App should switch back to the
// company-list screen without creating anything.
type cancelCompanyFormMsg struct{}

func (m *companyFormModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	if msg.Type == tea.KeyEsc {
		return nil, cancelCompanyFormMsg{}
	}
	if msg.Type == tea.KeyTab {
		m.inputs[m.focus].Blur()
		m.focus = (m.focus + 1) % formFieldCount
		m.inputs[m.focus].Focus()
		return nil, nil
	}
	if msg.Type == tea.KeyEnter {
		name := m.inputs[formFieldName].Value()
		sourceRef := m.inputs[formFieldSourceRef].Value()
		if name == "" || sourceRef == "" {
			return nil, nil
		}
		return createCompany(m.store, name, sourceRef), nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return cmd, nil
}

func (m *companyFormModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Add company") + "\n")
	labels := []string{"Name", "Ashby slug"}
	for i, input := range m.inputs {
		label := fieldLabel
		if i == m.focus {
			label = focusedLabel
		}
		b.WriteString(label.Render(labels[i]+":") + " " + input.View() + "\n")
	}
	b.WriteString(helpStyle.Render("tab: next field  enter: save  esc: cancel"))
	return b.String()
}
