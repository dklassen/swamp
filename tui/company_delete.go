package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// companyDeleteModel is the delete confirmation the company list draws
// over itself (d). Deleting closes every open posting the company has,
// too much for one stray key.
type companyDeleteModel struct {
	companyService *service.CompanyService

	company      message.Company
	openPostings int
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't send another.
	confirmed bool

	// instance is the delete's source, so its result is told from an
	// earlier confirmation's.
	instance message.ScreenInstance
}

func newCompanyDeleteModel(companies *service.CompanyService, company message.Company, openPostings int) *companyDeleteModel {
	return &companyDeleteModel{companyService: companies, company: company, openPostings: openPostings, instance: message.NewScreenInstance()}
}

// keyPressed handles a key, and says whether the confirmation closes.
func (m *companyDeleteModel) keyPressed(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch key.String() {
	case "esc", "enter", "n":
		return nil, true
	case "y":
		if !m.confirmed {
			m.confirmed = true
			companies, src, id := m.companyService, message.Source{Instance: m.instance}, m.company.ID
			return func() tea.Msg {
				deleted, err := companies.Delete(context.Background(), id)
				if err != nil {
					return message.New(src, message.Subject{Type: message.TypeCompany, ID: id}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(err))
				}
				return message.Deleted(src, id, deleted.At, companySnapshot(deleted.Before))
			}, false
		}
	}
	return nil, false
}

// View is a box, at most width wide, for the list to draw over itself.
func (m *companyDeleteModel) View(width int) string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Delete "+m.company.Name+"?") + " (" + m.company.Source + ")\n")
	fmt.Fprintf(&b, "Closes its open postings (%d) and keeps its applications.\n\n", m.openPostings)
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(model.FieldLabel.Render("y") + ": delete  " + model.FieldLabel.Render("n/esc/enter") + ": keep it")
	}
	return confirmBox(b.String(), width)
}
