package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/store"
)

// companyDeleteModel is the delete confirmation (d on the company list).
// Deleting closes every open posting the company has, too much for one
// stray key.
type companyDeleteModel struct {
	company      store.Company
	openPostings int
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't send another.
	confirmed bool
}

func newCompanyDeleteModel(company store.Company, openPostings int) companyDeleteModel {
	return companyDeleteModel{company: company, openPostings: openPostings}
}

// enterCompanyDeleteMsg signals that App should open the delete
// confirmation for this company.
type enterCompanyDeleteMsg struct {
	company store.Company
}

// cancelCompanyDeleteMsg signals that App should go back to the company
// list.
type cancelCompanyDeleteMsg struct{}

// confirmCompanyDeleteMsg signals that App should delete the company.
type confirmCompanyDeleteMsg struct {
	company store.Company
}

func (m *companyDeleteModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyEsc, msg.Type == tea.KeyEnter, msg.String() == "n":
		return nil, cancelCompanyDeleteMsg{}
	case msg.String() == "y" && !m.confirmed:
		m.confirmed = true
		return nil, confirmCompanyDeleteMsg{company: m.company}
	}
	return nil, nil
}

var confirmBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("214")).
	Padding(0, 1)

// View is a box for App to draw over the company list, at most width wide.
func (m *companyDeleteModel) View(width int) string {
	var b strings.Builder
	b.WriteString(fieldLabel.Render("Delete "+m.company.Name+"?") + " (" + m.company.Source + ")\n")
	fmt.Fprintf(&b, "Closes its open postings (%d) and keeps its applications.\n\n", m.openPostings)
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(fieldLabel.Render("y") + ": delete  " + fieldLabel.Render("n/esc/enter") + ": keep it")
	}
	style := confirmBoxStyle
	if w := style.GetHorizontalFrameSize(); width > w && lipgloss.Width(b.String())+w > width {
		style = style.Width(width - w)
	}
	return style.Render(b.String())
}
