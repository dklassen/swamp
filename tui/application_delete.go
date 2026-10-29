package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationDeleteModel is the delete confirmation, a box over the screen
// it was opened from (d on the home list, D on application detail), for an
// application started by accident. Confirming soft-deletes it: it leaves
// every list and the posting can start a fresh one, while its history,
// reviews and drafts are kept.
type applicationDeleteModel struct {
	application posting.ActiveApplication
	// from is the screen the box is drawn over, and where n goes back to.
	from screen
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't run it twice.
	confirmed bool
	instance  message.ScreenInstance
}

func newApplicationDeleteModel(application posting.ActiveApplication, from screen) applicationDeleteModel {
	return applicationDeleteModel{application: application, from: from, instance: message.NewScreenInstance()}
}

// enterApplicationDeleteMsg signals that App should open the delete
// confirmation for this application.
type enterApplicationDeleteMsg struct {
	application posting.ActiveApplication
}

// cancelApplicationDeleteMsg signals that App should go back to where the
// confirmation was opened.
type cancelApplicationDeleteMsg struct{}

// confirmApplicationDeleteMsg signals that App should delete the
// application.
type confirmApplicationDeleteMsg struct {
	application posting.ActiveApplication
}

func (m *applicationDeleteModel) Update(msg tea.KeyPressMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.String() == "esc", msg.String() == "enter", msg.String() == "n":
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
	b.WriteString(model.FieldLabel.Render("Delete the application for "+m.application.Posting.Title+"?") + " (" + m.application.CompanyName + ")\n")
	b.WriteString("Status: " + applicationStatusLabel(m.application.Status) + "\n")
	b.WriteString("Its status history, reviews and drafts are kept.\n\n")
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(model.FieldLabel.Render("y") + ": delete  " + model.FieldLabel.Render("n/esc/enter") + ": keep it")
	}
	return confirmBox(b.String(), width)
}

// deleteApplication soft-deletes an application. Its documents stay on
// disk for a later restore; with IDs never reused, nothing else can pick
// them up.
func deleteApplication(applications *service.ApplicationService, src message.Source, applicationID int64) tea.Cmd {
	return func() tea.Msg {
		a, err := applications.Delete(context.Background(), applicationID)
		return applicationWritten(src, applicationID, message.OpDeleted, a, err)
	}
}

// applicationReloadedForDeleteMsg carries the application the delete
// confirmation describes, as stored when delete was pressed on from.
type applicationReloadedForDeleteMsg struct {
	application posting.ActiveApplication
	from        screen
	err         error
}

// reloadApplicationForDelete refreshes view's application row before the
// confirmation shows it: sync or an agent may have changed its status, or
// deleted it, since the list loaded. The posting and
// company parts don't affect what's deleted, so they're kept as loaded.
func reloadApplicationForDelete(applications *service.ApplicationService, view posting.ActiveApplication, from screen) tea.Cmd {
	return func() tea.Msg {
		app, err := applications.Get(context.Background(), view.ID)
		if errors.Is(err, store.ErrNotFound) {
			return applicationReloadedForDeleteMsg{from: from, err: errors.New("this application was already deleted")}
		}
		if err != nil {
			return applicationReloadedForDeleteMsg{from: from, err: err}
		}
		view.Application = *app
		return applicationReloadedForDeleteMsg{application: view, from: from}
	}
}
