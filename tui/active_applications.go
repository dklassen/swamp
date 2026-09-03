package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/store"
)

// activeApplicationListModel drives the active-applications screen -- the
// app's home screen (see decisions.log, #43): every application not at a
// terminal dead-end status (rejected, offer_declined), across every
// company, in one place. It holds only its own private cursor -- the
// application list itself is domain data owned by App, passed in on
// every call, never cached here. Application-specific actions
// (editing/reviewing documents, viewing the related posting) live one
// level down, on applicationDetailModel -- entered via enter (see
// enterApplicationDetailMsg below); this screen keeps only
// status-setting as a quick shortcut for fast triage across many
// applications (see decisions.log, the #86 follow-up).
type activeApplicationListModel struct {
	cursor int
}

func newActiveApplicationListModel() activeApplicationListModel {
	return activeApplicationListModel{}
}

// enterApplicationDetailMsg signals that App should switch to the
// application-detail screen for this application.
type enterApplicationDetailMsg struct {
	application store.ApplicationView
}

// Update handles one key press. The returned tea.Cmd (if non-nil) is a
// real async command for App to run through bubbletea as usual. The
// returned tea.Msg (if non-nil) is an intent for App to apply
// synchronously -- see companyListModel.Update for the same convention.
func (m *activeApplicationListModel) Update(msg tea.KeyMsg, apps []store.ApplicationView) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(apps)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "q":
		return tea.Quit, nil
	case msg.String() == "c":
		return nil, backToCompanyListMsg{}
	case msg.String() == "s":
		if m.cursor < len(apps) {
			a := apps[m.cursor]
			return nil, enterApplicationStatusMsg{postingID: a.Posting.ID, currentStatus: a.Status}
		}
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(apps) {
			return nil, enterApplicationDetailMsg{application: apps[m.cursor]}
		}
	}
	return nil, nil
}

func (m *activeApplicationListModel) View(apps []store.ApplicationView, listRows int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Active Applications") + "\n")
	if len(apps) == 0 {
		b.WriteString("No active applications. Press 'c' to browse companies.\n")
	} else {
		rows := listRows - postingTableChromeLines
		if rows < 0 {
			rows = 0
		}
		start, end := visibleWindow(m.cursor, len(apps), rows)
		cursorRow := m.cursor - start
		t := table.New().
			Headers("Company", "Title", "Status", "Review").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		for i := start; i < end; i++ {
			a := apps[i]
			t.Row(
				truncateCol(a.CompanyName, departmentColWidth),
				truncateCol(a.Posting.Title, titleColWidth),
				a.Status.String(),
				reviewGlyphSummary(a.LatestReviews),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: application detail  s: status  c: companies  q: quit"))
	return b.String()
}

// resetCursorIfOutOfBounds resets the cursor to the top if it's no
// longer a valid index into n applications -- used after a status
// change moves an application to a terminal status and it drops out of
// the list.
func (m *activeApplicationListModel) resetCursorIfOutOfBounds(n int) {
	if m.cursor >= n {
		m.cursor = 0
	}
}
