package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationDeleteModel is the delete confirmation, a box over the screen
// it was opened from (d on the home list, D on application detail), for an
// application started by accident. Confirming soft-deletes it: it leaves
// every list and the posting can start a fresh one, while its history,
// reviews and drafts are kept.
type applicationDeleteModel struct {
	applications *service.ApplicationService

	detail message.ApplicationDetail
	// confirmed is set once y is pressed, so a second y while the delete
	// is in flight doesn't send another.
	confirmed bool

	focused  bool
	width    int
	instance message.ScreenInstance
}

func newApplicationDeleteModel(applications *service.ApplicationService, d message.ApplicationDetail, width int) *applicationDeleteModel {
	return &applicationDeleteModel{applications: applications, detail: d, width: width, instance: message.NewScreenInstance()}
}

func (m *applicationDeleteModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationDeleteModel) Focused() bool { return m.focused }

func (m *applicationDeleteModel) Routes() []*model.Route {
	return []*model.Route{
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.applicationDeleted, model.About(message.TypeApplication, m.detail.Application.ID), model.OnOp(message.OpDeleted)),
		model.Always(m.deleteFailed, model.From(m.instance)),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *applicationDeleteModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationDeleteModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

// applicationDeleted closes the confirmation, whoever deleted its
// application, and says which it was.
func (m *applicationDeleteModel) applicationDeleted(e message.Event[message.Changed[message.Application]]) tea.Cmd {
	if e.Meta.Source.Instance != m.instance {
		return m.goneElsewhere()
	}
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), "Deleted the application for "+m.detail.Posting.Title))
}

// deleteFailed closes the confirmation too: a failed delete's y is spent,
// and the banner shows the error.
func (m *applicationDeleteModel) deleteFailed(e message.Event[message.Error]) tea.Cmd {
	if e.Payload.Code == message.ErrNotFound {
		return m.goneElsewhere()
	}
	return message.Pop(m.src())
}

func (m *applicationDeleteModel) goneElsewhere() tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), "The application for "+m.detail.Posting.Title+" was deleted elsewhere"))
}

func (m *applicationDeleteModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width = e.Payload.Value.Width
	return nil
}

func (m *applicationDeleteModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the confirmation handles is
// registered.
func (m *applicationDeleteModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationDeleteModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "enter", "n":
		return message.Pop(m.src())
	case "y":
		if !m.confirmed {
			m.confirmed = true
			return deleteApplication(m.applications, m.src(), m.detail.Application.ID)
		}
	}
	return nil
}

// View is a box for App to draw over the screen it was opened from.
func (m *applicationDeleteModel) View() tea.View {
	var b strings.Builder
	b.WriteString(model.FieldLabel.Render("Delete the application for "+m.detail.Posting.Title+"?") + " (" + m.detail.CompanyName + ")\n")
	b.WriteString("Status: " + applicationStatusLabel(applicationOf(m.detail.Application).Status) + "\n")
	b.WriteString("Its status history, reviews and drafts are kept.\n\n")
	if m.confirmed {
		b.WriteString("Deleting...")
	} else {
		b.WriteString(model.FieldLabel.Render("y") + ": delete  " + model.FieldLabel.Render("n/esc/enter") + ": keep it")
	}
	return tea.NewView(confirmBox(b.String(), m.width))
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
