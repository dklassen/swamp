package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// companyDeleteModel is the delete confirmation (d on the company list).
// Deleting closes every open posting the company has, too much for one
// stray key.
type companyDeleteModel struct {
	companyService *service.CompanyService

	company      company.Company
	openPostings int
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't send another.
	confirmed bool

	focused  bool
	width    int
	instance message.ScreenInstance
}

func newCompanyDeleteModel(companies *service.CompanyService, company company.Company, openPostings, width int) *companyDeleteModel {
	return &companyDeleteModel{companyService: companies, company: company, openPostings: openPostings, width: width, instance: message.NewScreenInstance()}
}

func (m *companyDeleteModel) Instance() message.ScreenInstance { return m.instance }

func (m *companyDeleteModel) Focused() bool { return m.focused }

func (m *companyDeleteModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.companyDeleted, model.About(message.TypeCompany, m.company.ID), model.OnOp(message.OpDeleted)),
		model.Always(m.deleteFailed, model.From(m.instance)),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *companyDeleteModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// companyDeleted closes the confirmation, whoever deleted its company, and
// clears an earlier failure from the banner.
func (m *companyDeleteModel) companyDeleted(message.Event[message.Changed[message.Company]]) tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return message.CloseCompanyDeleteMsg{} },
		message.ReportStatus(message.Source{Instance: m.instance}, ""),
	)
}

// deleteFailed closes the confirmation too: a failed delete's y is spent,
// and the banner shows the error.
func (m *companyDeleteModel) deleteFailed(message.Event[message.Error]) tea.Cmd {
	return func() tea.Msg { return message.CloseCompanyDeleteMsg{} }
}

func (m *companyDeleteModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width = e.Payload.Value.Width
	return nil
}

func (m *companyDeleteModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the confirmation handles is
// registered.
func (m *companyDeleteModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *companyDeleteModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "enter", "n":
		return func() tea.Msg { return message.CloseCompanyDeleteMsg{} }
	case "y":
		if !m.confirmed {
			m.confirmed = true
			return deleteCompany(m.companyService, message.Source{Instance: m.instance}, m.company.ID)
		}
	}
	return nil
}

// View is a box for App to draw over the company list.
func (m *companyDeleteModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Delete "+m.company.Name+"?") + " (" + m.company.Source + ")\n")
	fmt.Fprintf(&b, "Closes its open postings (%d) and keeps its applications.\n\n", m.openPostings)
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(model.FieldLabel.Render("y") + ": delete  " + model.FieldLabel.Render("n/esc/enter") + ": keep it")
	}
	return tea.NewView(confirmBox(b.String(), m.width))
}
