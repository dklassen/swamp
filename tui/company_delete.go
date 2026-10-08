package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

func (m *companyDeleteModel) View() string {
	var b strings.Builder
	b.WriteString(fieldLabel.Render("Company:") + " " + m.company.Name + " (" + m.company.Source + ")\n")
	b.WriteString(fieldLabel.Render("Open postings:") + fmt.Sprintf(" %d\n\n", m.openPostings))
	b.WriteString("The company leaves the list and its open postings are closed. Its applications are kept.\n\n")
	if m.confirmed {
		b.WriteString("Deleting...\n")
	} else {
		b.WriteString("Delete it? " + fieldLabel.Render("y") + ": delete  " + fieldLabel.Render("n/esc/enter") + ": keep it\n")
	}
	return b.String()
}
