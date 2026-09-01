package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// activeApplicationListModel drives the active-applications screen -- the
// app's home screen (see decisions.log, #43): every application not at a
// terminal dead-end status (rejected, offer_declined), across every
// company, in one place. It holds only the dependencies it needs to
// dispatch its own commands (store, documents) and its own private
// cursor -- the application list itself is domain data owned by App,
// passed in on every call, never cached here.
type activeApplicationListModel struct {
	store     *store.Store
	documents *documents.Store
	cursor    int
}

func newActiveApplicationListModel(s *store.Store, docs *documents.Store) activeApplicationListModel {
	return activeApplicationListModel{store: s, documents: docs}
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
	case msg.String() == "l":
		if m.cursor < len(apps) {
			return m.openDocument(apps[m.cursor], false), nil
		}
	case msg.String() == "r":
		if m.cursor < len(apps) {
			return m.openDocument(apps[m.cursor], true), nil
		}
	}
	return nil, nil
}

// openDocument ensures the application's document directory exists (most
// editors create the file itself on save, but not the directory) and
// returns a command that opens the cover letter (resume=false) or resume
// (resume=true) in $EDITOR. EnsureDir is local filesystem I/O -- cheap
// enough to call synchronously here rather than through a separate
// tea.Cmd round trip, same reasoning postingDetailContent's docs.Status
// call already relies on (see decisions.log).
func (m *activeApplicationListModel) openDocument(a store.ApplicationView, resume bool) tea.Cmd {
	paths, err := m.documents.EnsureDir(a.ID)
	if err != nil {
		return func() tea.Msg { return editorClosedMsg{err: err} }
	}
	path := paths.CoverLetter
	if resume {
		path = paths.Resume
	}
	return openInEditor(path)
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
			Headers("Company", "Title", "Status").
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
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  l: cover letter  r: resume  s: status  c: companies  q: quit"))
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
