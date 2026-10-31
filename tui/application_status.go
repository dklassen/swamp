package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// applicationStatuses is the full fixed set of legal application statuses,
// per application.All -- application.Status (a Go enum) is
// the sole source of truth for valid values now; the DB column has no
// CHECK constraint of its own to stay in sync with. The schema encodes no transition graph -- every status is
// reachable from every other -- so the status-select screen offers all of
// them unconditionally rather than a hand-maintained "valid next status"
// list.
var applicationStatuses = application.All()

// applicationStatusIndex returns status's position in applicationStatuses,
// or 0 if not found -- used to point the status-select cursor at the
// application's current status when the screen is opened.
func applicationStatusIndex(status application.Status) int {
	for i, s := range applicationStatuses {
		if s == status {
			return i
		}
	}
	return 0
}

// applicationStatusModel picks a new status for one application and saves
// it.
type applicationStatusModel struct {
	applications *service.ApplicationService
	application  message.Application
	cursor       int
	focused      bool
	instance     message.ScreenInstance
}

// newApplicationStatusModel opens on app, with the cursor on its status.
func newApplicationStatusModel(applications *service.ApplicationService, app message.Application) *applicationStatusModel {
	return &applicationStatusModel{
		applications: applications,
		application:  app,
		cursor:       applicationStatusIndex(applicationOf(app).Status),
		instance:     message.NewScreenInstance(),
	}
}

func (m *applicationStatusModel) src() message.Source { return message.Source{Instance: m.instance} }

func (m *applicationStatusModel) Instance() message.ScreenInstance { return m.instance }

func (m *applicationStatusModel) Focused() bool { return m.focused }

func (m *applicationStatusModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.saved, model.From(m.instance)),
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.focusChanged),
	}
}

// saved closes the picker. A failed save sends an Error instead, which
// leaves it open.
func (m *applicationStatusModel) saved(message.Event[message.Changed[message.Application]]) tea.Cmd {
	return m.close()
}

func (m *applicationStatusModel) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the picker handles is registered.
func (m *applicationStatusModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *applicationStatusModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *applicationStatusModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "down", "j":
		if m.cursor < len(applicationStatuses)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "esc", "b":
		return m.close()
	case "enter":
		return updateApplicationStatus(m.applications, m.src(), m.application.ID, applicationStatuses[m.cursor])
	}
	return nil
}

// close takes a failed save's error from the banner with the picker.
func (m *applicationStatusModel) close() tea.Cmd {
	return tea.Batch(message.Pop(m.src()), message.ReportStatus(m.src(), ""))
}

func (m *applicationStatusModel) crumbs(message.Screen) []string {
	return []string{"Set status"}
}

func (m *applicationStatusModel) View() tea.View {
	var b strings.Builder
	for i, st := range applicationStatuses {
		if i == m.cursor {
			b.WriteString(model.CursorStyle.Render("> "+applicationStatusLabel(st)) + "\n")
		} else {
			b.WriteString("  " + applicationStatusLabel(st) + "\n")
		}
	}
	b.WriteString(model.HelpStyle.Render("↑/↓ (j/k): select  enter: save  esc/b: cancel"))
	return tea.NewView(b.String())
}

// applicationStatusLabel renders an ApplicationStatus as display text.
// ApplicationStatus.String() is the value persisted to the DB (see
// application.Status), so it can't be prettified at the source
// without changing what's written to applications.status -- the
// presentation form lives here instead, the same way document.Type.Label
// handles document.Type.
//
// The default case falls back to String() so an unmapped status still
// renders something rather than an empty cell; TestApplicationStatus
// Label_CoversEveryStatus fails if a new status ever reaches it.
func applicationStatusLabel(status application.Status) string {
	switch status {
	case application.Started:
		return "Started"
	case application.Submitted:
		return "Submitted"
	case application.Interviewing:
		return "Interviewing"
	case application.Rejected:
		return "Rejected"
	case application.OfferReceived:
		return "Offer received"
	case application.OfferAccepted:
		return "Offer accepted"
	case application.OfferDeclined:
		return "Offer declined"
	case application.PostingClosed:
		return "Posting closed"
	case application.Withdrawn:
		return "Withdrawn"
	default:
		return status.String()
	}
}
