package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
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

// applicationStatusModel drives the application-status-select screen for
// a single application. It holds the service it needs to save the new
// status, the application it's editing (fixed for this screen's
// lifetime), and its own private cursor -- no other screen reads or
// writes this state.
type applicationStatusModel struct {
	applications  *service.ApplicationService
	applicationID int64
	cursor        int
	instance      screenInstance
}

// newApplicationStatusModel returns a status-select screen for
// applicationID, with the cursor seeded at currentStatus's position.
func newApplicationStatusModel(applications *service.ApplicationService, applicationID int64, currentStatus application.Status, instance screenInstance) applicationStatusModel {
	return applicationStatusModel{applications: applications, applicationID: applicationID, cursor: applicationStatusIndex(currentStatus), instance: instance}
}

// cancelApplicationStatusMsg signals that App should switch back to the
// posting-detail screen without saving.
type cancelApplicationStatusMsg struct{}

func (m *applicationStatusModel) Update(msg tea.KeyPressMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.String() == "down", msg.String() == "j":
		if m.cursor < len(applicationStatuses)-1 {
			m.cursor++
		}
	case msg.String() == "up", msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "esc", msg.String() == "b":
		return nil, cancelApplicationStatusMsg{}
	case msg.String() == "enter":
		status := applicationStatuses[m.cursor]
		return updateApplicationStatus(m.applications, m.applicationID, status, m.instance), nil
	}
	return nil, nil
}

func (m *applicationStatusModel) View() string {
	var b strings.Builder
	for i, st := range applicationStatuses {
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+applicationStatusLabel(st)) + "\n")
		} else {
			b.WriteString("  " + applicationStatusLabel(st) + "\n")
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: save  esc/b: cancel"))
	return b.String()
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

// applicationStatusLoadedMsg carries the stored status the status form
// opens on, and the screen that asked for it.
type applicationStatusLoadedMsg struct {
	applicationID int64
	status        application.Status
	from          screen
	err           error
}

// loadApplicationStatus reads the application's status as it is now, not
// as the screen that opens the form loaded it: sync or an agent may have
// changed it since, and the form would undo that on save.
func loadApplicationStatus(applications *service.ApplicationService, applicationID int64, from screen) tea.Cmd {
	return func() tea.Msg {
		app, err := applications.Get(context.Background(), applicationID)
		if errors.Is(err, store.ErrNotFound) {
			err = errors.New("this application was deleted in the meantime")
		}
		if err != nil {
			return applicationStatusLoadedMsg{applicationID: applicationID, from: from, err: err}
		}
		return applicationStatusLoadedMsg{applicationID: applicationID, status: app.Status, from: from}
	}
}
