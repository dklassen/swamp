package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// applicationDeleteModel is the delete confirmation, a box over the screen
// it was opened from (d on the home list, D on application detail), for an
// application started by accident. Confirming soft-deletes it: it leaves
// every list and the posting can start a fresh one, while its history,
// reviews and drafts are kept.
type applicationDeleteModel struct {
	application store.ApplicationView
	// from is the screen the box is drawn over, and where n goes back to.
	from screen
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't run it twice.
	confirmed bool
}

func newApplicationDeleteModel(application store.ApplicationView, from screen) applicationDeleteModel {
	return applicationDeleteModel{application: application, from: from}
}

// enterApplicationDeleteMsg signals that App should open the delete
// confirmation for this application.
type enterApplicationDeleteMsg struct {
	application store.ApplicationView
}

// cancelApplicationDeleteMsg signals that App should go back to where the
// confirmation was opened.
type cancelApplicationDeleteMsg struct{}

// confirmApplicationDeleteMsg signals that App should delete the
// application.
type confirmApplicationDeleteMsg struct {
	application store.ApplicationView
}

func (m *applicationDeleteModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyEsc, msg.Type == tea.KeyEnter, msg.String() == "n":
		return nil, cancelApplicationDeleteMsg{}
	case msg.String() == "y" && !m.confirmed:
		m.confirmed = true
		return nil, confirmApplicationDeleteMsg{application: m.application}
	}
	return nil, nil
}

// View is a box for App to draw over m.from, at most width wide.
func (m *applicationDeleteModel) View(width int) string {
	var b strings.Builder
	b.WriteString(fieldLabel.Render("Delete the application for "+m.application.Posting.Title+"?") + " (" + m.application.CompanyName + ")\n")
	b.WriteString("Status: " + applicationStatusLabel(m.application.Status) + "\n")
	b.WriteString("Its status history, reviews and drafts are kept.\n\n")
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(fieldLabel.Render("y") + ": delete  " + fieldLabel.Render("n/esc/enter") + ": keep it")
	}
	return confirmBox(b.String(), width)
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
// confirmation describes, as stored when delete was pressed on from.
type applicationReloadedForDeleteMsg struct {
	application store.ApplicationView
	from        screen
	err         error
}

// reloadApplicationForDelete refreshes view's application row before the
// confirmation shows it: sync or an agent may have changed its status, or
// deleted it, since the list loaded. The posting and
// company parts don't affect what's deleted, so they're kept as loaded.
func reloadApplicationForDelete(s *store.Store, view store.ApplicationView, from screen) tea.Cmd {
	return func() tea.Msg {
		application, err := s.GetApplicationByID(context.Background(), view.ID)
		if errors.Is(err, store.ErrNotFound) {
			return applicationReloadedForDeleteMsg{from: from, err: errors.New("this application was already deleted")}
		}
		view.Application = application
		return applicationReloadedForDeleteMsg{application: view, from: from, err: err}
	}
}
