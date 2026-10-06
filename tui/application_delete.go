package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// applicationDeleteModel is the delete confirmation (D on application
// detail), for an application started by accident. Confirming soft-deletes
// it: it leaves every list and the posting can start a fresh one, while
// its history, reviews and drafts are kept.
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

	b.WriteString("The application leaves every list, and the posting can be started again from scratch.\n")
	b.WriteString("Its status history and reviews are kept")
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
		b.WriteString(", and so are its drafts:\n")
		for _, path := range drafted {
			b.WriteString("  " + path + "\n")
		}
	}
	b.WriteString("The posting stays as it is. There is no way to restore it from Swamp yet.\n\n")

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

// deleteApplication soft-deletes application. Its documents stay on disk
// for a later restore; with IDs never reused, nothing else can pick them
// up.
func deleteApplication(s *store.Store, application store.ApplicationView) tea.Cmd {
	return func() tea.Msg {
		err := s.DeleteApplication(context.Background(), application.ID)
		return applicationDeletedMsg{application: application, err: err}
	}
}

// applicationReloadedForDeleteMsg carries the application the delete
// confirmation describes, as stored when D was pressed.
type applicationReloadedForDeleteMsg struct {
	application store.ApplicationView
	err         error
}

// reloadApplicationForDelete refreshes view's application row before the
// confirmation shows it: sync or an agent may have changed its status, or
// deleted it, since the list loaded. The posting and
// company parts don't affect what's deleted, so they're kept as loaded.
func reloadApplicationForDelete(s *store.Store, view store.ApplicationView) tea.Cmd {
	return func() tea.Msg {
		application, err := s.GetApplicationByID(context.Background(), view.ID)
		if errors.Is(err, store.ErrNotFound) {
			return applicationReloadedForDeleteMsg{err: errors.New("this application was already deleted")}
		}
		view.Application = application
		return applicationReloadedForDeleteMsg{application: view, err: err}
	}
}
