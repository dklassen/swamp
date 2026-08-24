package tui

import (
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
}

// newApplicationStatusModel returns a status-select screen for
// postingID, with the cursor seeded at currentStatus's position.
func newApplicationStatusModel(s *store.Store, postingID int64, currentStatus store.ApplicationStatus) applicationStatusModel {
	return applicationStatusModel{store: s, postingID: postingID, cursor: applicationStatusIndex(currentStatus)}
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
		return updateApplicationStatus(m.store, m.postingID, status), nil
	}
	return nil, nil
}

func (m *applicationStatusModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Set application status") + "\n")
	for i, st := range applicationStatuses {
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+st.String()) + "\n")
		} else {
			b.WriteString("  " + st.String() + "\n")
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: save  esc/b: cancel"))
	return b.String()
}
