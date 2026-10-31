package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationDeleteModel is the delete confirmation a screen draws over
// itself (d on the home list, D on application detail), for an application
// started by accident. Confirming soft-deletes it: it leaves every list and
// the posting can start a fresh one, while its history, reviews and drafts
// are kept.
type applicationDeleteModel struct {
	applications *service.ApplicationService

	detail message.ApplicationDetail
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't send another.
	confirmed bool

	// instance is the delete's source, so its result is told from an
	// earlier confirmation's.
	instance message.ScreenInstance
}

func newApplicationDeleteModel(applications *service.ApplicationService, d message.ApplicationDetail) *applicationDeleteModel {
	return &applicationDeleteModel{applications: applications, detail: d, instance: message.NewScreenInstance()}
}

func (m *applicationDeleteModel) src() message.Source { return message.Source{Instance: m.instance} }

// deleted says who deleted its application: it, or someone elsewhere.
func (m *applicationDeleteModel) deleted(e message.Event[message.Changed[message.Application]]) tea.Cmd {
	if e.Meta.Source.Instance != m.instance {
		return m.goneElsewhere()
	}
	return message.ReportStatus(m.src(), "Deleted the application for "+m.detail.Posting.Title)
}

// failed says the application was already gone, if that's why its delete
// failed; otherwise the banner shows the error.
func (m *applicationDeleteModel) failed(e message.Event[message.Error]) tea.Cmd {
	if e.Payload.Code == message.ErrNotFound {
		return m.goneElsewhere()
	}
	return nil
}

func (m *applicationDeleteModel) goneElsewhere() tea.Cmd {
	return message.ReportStatus(m.src(), "The application for "+m.detail.Posting.Title+" was deleted elsewhere")
}

// keyPressed handles a key, and says whether the confirmation closes.
func (m *applicationDeleteModel) keyPressed(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch key.String() {
	case "esc", "enter", "n":
		return nil, true
	case "y":
		if !m.confirmed {
			m.confirmed = true
			return deleteApplication(m.applications, m.src(), m.detail.Application.ID), false
		}
	}
	return nil, false
}

// View is a box, at most width wide, for its owner to draw over itself.
func (m *applicationDeleteModel) View(width int) string {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Delete the application for "+m.detail.Posting.Title+"?") + " (" + m.detail.CompanyName + ")\n")
	b.WriteString("Status: " + applicationStatusLabel(applicationOf(m.detail.Application).Status) + "\n")
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
