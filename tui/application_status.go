package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// applicationStatuses is the full fixed set of legal application statuses,
// per store.ApplicationStatuses -- store.ApplicationStatus (a Go enum) is
// the sole source of truth for valid values now (see decisions.log,
// 2026-08-19); the DB column has no CHECK constraint of its own to stay in
// sync with. The schema encodes no transition graph -- every status is
// reachable from every other -- so the status-select screen offers all of
// them unconditionally rather than a hand-maintained "valid next status"
// list.
var applicationStatuses = store.ApplicationStatuses()

// applicationStatusIndex returns status's position in applicationStatuses,
// or 0 if not found -- used to point the status-select cursor at the
// application's current status when the screen is opened.
func applicationStatusIndex(status store.ApplicationStatus) int {
	for i, s := range applicationStatuses {
		if s == status {
			return i
		}
	}
	return 0
}

// applicationStatusModel drives the application-status-select screen for
// a single posting's application. It holds the store it needs to save the
// new status, the posting it's editing (fixed for this screen's
// lifetime), and its own private cursor -- no other screen reads or
// writes this state.
type applicationStatusModel struct {
	store     *store.Store
	postingID int64
	cursor    int
	instance  screenInstance
}

// newApplicationStatusModel returns a status-select screen for
// postingID, with the cursor seeded at currentStatus's position.
func newApplicationStatusModel(s *store.Store, postingID int64, currentStatus store.ApplicationStatus, instance screenInstance) applicationStatusModel {
	return applicationStatusModel{store: s, postingID: postingID, cursor: applicationStatusIndex(currentStatus), instance: instance}
}

// cancelApplicationStatusMsg signals that App should switch back to the
// posting-detail screen without saving.
type cancelApplicationStatusMsg struct{}

func (m *applicationStatusModel) Update(msg tea.KeyMsg) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(applicationStatuses)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, cancelApplicationStatusMsg{}
	case msg.Type == tea.KeyEnter:
		status := applicationStatuses[m.cursor]
		return updateApplicationStatus(m.store, m.postingID, status, m.instance), nil
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
// store.ApplicationStatus), so it can't be prettified at the source
// without changing what's written to applications.status -- the
// presentation form lives here instead, the same way documents.Type.Label
// handles documents.Type.
//
// The default case falls back to String() so an unmapped status still
// renders something rather than an empty cell; TestApplicationStatus
// Label_CoversEveryStatus fails if a new status ever reaches it.
func applicationStatusLabel(status store.ApplicationStatus) string {
	switch status {
	case store.ApplicationStatusStarted:
		return "Started"
	case store.ApplicationStatusSubmitted:
		return "Submitted"
	case store.ApplicationStatusInterviewing:
		return "Interviewing"
	case store.ApplicationStatusRejected:
		return "Rejected"
	case store.ApplicationStatusOfferReceived:
		return "Offer received"
	case store.ApplicationStatusOfferAccepted:
		return "Offer accepted"
	case store.ApplicationStatusOfferDeclined:
		return "Offer declined"
	case store.ApplicationStatusPostingClosed:
		return "Posting closed"
	case store.ApplicationStatusWithdrawn:
		return "Withdrawn"
	default:
		return status.String()
	}
}

// applicationStatusLoadedMsg carries the stored status the status form
// opens on, and the screen that asked for it.
type applicationStatusLoadedMsg struct {
	postingID int64
	status    store.ApplicationStatus
	from      screen
	err       error
}

// loadApplicationStatus reads postingID's application status as it is
// now, not as the screen that opens the form loaded it: sync or an agent
// may have changed it since, and the form would undo that on save (RFC
// 0007, H5).
func loadApplicationStatus(s *store.Store, postingID int64, from screen) tea.Cmd {
	return func() tea.Msg {
		application, err := s.GetApplication(context.Background(), postingID)
		if errors.Is(err, store.ErrNotFound) {
			err = errors.New("this application was deleted in the meantime")
		}
		return applicationStatusLoadedMsg{postingID: postingID, status: application.Status, from: from, err: err}
	}
}
