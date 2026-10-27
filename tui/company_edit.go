package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// companyEditModel drives the edit-company screen: name only. source/
// source_ref are locked after creation and have no
// field here at all -- not just disabled, since there's nothing for this
// screen to do with them.
type companyEditModel struct {
	companies *service.CompanyService
	companyID int64
	nameInput textinput.Model
	// saving is true from the moment Enter dispatches updateCompanyName
	// until its result arrives. bubbletea has no way to cancel an
	// already-dispatched command, so Esc can't actually stop an in-flight
	// save -- it's blocked instead, so "esc: cancel" stays true rather
	// than racing the save (#37).
	saving bool

	focused  bool
	instance message.ScreenInstance
}

// newCompanyEditModel returns an edit screen for companyID, seeded with
// its current name.
func newCompanyEditModel(companies *service.CompanyService, companyID int64, name string) *companyEditModel {
	input := textinput.New()
	input.SetValue(name)
	input.Focus()
	return &companyEditModel{companies: companies, companyID: companyID, nameInput: input, instance: message.NewScreenInstance()}
}

func (m *companyEditModel) source() message.Source {
	return message.Source{Instance: m.instance}
}

func (m *companyEditModel) Instance() message.ScreenInstance { return m.instance }

func (m *companyEditModel) Focused() bool { return m.focused }

func (m *companyEditModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.companyRenamed, model.From(m.instance)),
		model.Always(m.renameFailed, model.From(m.instance)),
		model.Always(m.focusChanged),
	}
}

func (m *companyEditModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *companyEditModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the edit handles is registered.
func (m *companyEditModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *companyEditModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.saving {
			return nil
		}
		return m.close()
	case "enter":
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			return nil
		}
		m.saving = true
		return updateCompanyName(m.companies, m.source(), m.companyID, name)
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd
}

// close takes a failed save's error from the banner with the edit.
func (m *companyEditModel) close() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return message.CloseCompanyEditMsg{} },
		message.ReportStatus(m.source(), ""),
	)
}

func (m *companyEditModel) companyRenamed(message.Event[message.Changed[message.Company]]) tea.Cmd {
	m.saving = false
	return m.close()
}

// renameFailed lets Esc cancel again, and leaves the edit open with the
// name as typed.
func (m *companyEditModel) renameFailed(message.Event[message.Error]) tea.Cmd {
	m.saving = false
	return nil
}

func (m *companyEditModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FocusedLabel.Render("Name:") + " " + m.nameInput.View() + "\n")
	b.WriteString(model.HelpStyle.Render("enter: save  esc: cancel"))
	return tea.NewView(b.String())
}

func (m *companyEditModel) paste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd
}
