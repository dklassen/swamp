package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// applicationDeleteModel drives the delete confirmation (#232), reached
// with D on application detail: for an application started by accident,
// e.g. on the wrong posting. Confirming removes the application, its
// history, reviews, export records and interview stages, and its drafted
// documents on disk. The posting itself is left exactly as it was.
type applicationDeleteModel struct {
	documents   *documents.Store
	application store.ApplicationView
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't run it twice.
	confirmed bool
}

func newApplicationDeleteModel(docs *documents.Store, application store.ApplicationView) applicationDeleteModel {
	return applicationDeleteModel{documents: docs, application: application}
}

// enterApplicationDeleteMsg signals that App should open the delete
// confirmation for this application.
type enterApplicationDeleteMsg struct {
	application store.ApplicationView
}

// cancelApplicationDeleteMsg signals that App should go back to
// application detail without deleting anything.
type cancelApplicationDeleteMsg struct{}

// confirmApplicationDeleteMsg signals that App should delete the
// application.
type confirmApplicationDeleteMsg struct {
	application store.ApplicationView
}

func (m *applicationDeleteModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyEsc, msg.String() == "n":
		return nil, cancelApplicationDeleteMsg{}
	case msg.String() == "y" && !m.confirmed:
		m.confirmed = true
		return nil, confirmApplicationDeleteMsg{application: m.application}
	}
	return nil, nil
}

func (m *applicationDeleteModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Delete application: "+m.application.Posting.Title) + "\n")
	b.WriteString(fieldLabel.Render("Company:") + " " + m.application.CompanyName + "\n")
	b.WriteString(fieldLabel.Render("Status:") + " " + applicationStatusLabel(m.application.Status) + "\n\n")

	b.WriteString("This removes the application's status history, reviews, export records and interview stages")
	var drafted []string
	status := m.documents.Status(m.application.ID)
	for _, documentType := range documents.Types() {
		if doc, err := status.Doc(documentType); err == nil && doc.Exists {
			drafted = append(drafted, doc.Path)
		}
	}
	if len(drafted) == 0 {
		b.WriteString(".\n")
	} else {
		b.WriteString(", and deletes its drafts:\n")
		for _, path := range drafted {
			b.WriteString("  " + path + "\n")
		}
	}
	b.WriteString("The posting stays as it is. This can't be undone.\n\n")

	if m.confirmed {
		b.WriteString("Deleting...\n")
	} else {
		b.WriteString("Delete it? " + fieldLabel.Render("y") + ": delete  " + fieldLabel.Render("n/esc") + ": keep it\n")
	}
	return b.String()
}

// applicationDeletedMsg reports the result of deleteApplication.
type applicationDeletedMsg struct {
	application store.ApplicationView
	err         error
}

// deleteApplication removes application's documents on disk, then the
// application itself. Documents go first: if that fails nothing has been
// deleted from the store, whereas a store delete that left the directory
// behind would orphan its drafts with no application to show them.
func deleteApplication(s *store.Store, docs *documents.Store, application store.ApplicationView) tea.Cmd {
	return func() tea.Msg {
		if err := docs.RemoveDir(application.ID); err != nil {
			return applicationDeletedMsg{application: application, err: err}
		}
		err := s.DeleteApplication(context.Background(), application.ID)
		return applicationDeletedMsg{application: application, err: err}
	}
}
