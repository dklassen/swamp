package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// companyListModel drives the company-list screen. It holds only the
// dependencies it needs to dispatch its own commands (store, syncer) and
// its own private cursor -- companies themselves are domain data owned
// by App and passed in on every call, never cached here.
type companyListModel struct {
	store  *store.Store
	syncer *sync.Syncer
	cursor int
}

func newCompanyListModel(s *store.Store, syncer *sync.Syncer) companyListModel {
	return companyListModel{store: s, syncer: syncer}
}

// enterCompanyFormMsg signals that App should switch to the company-form
// screen. companyListModel doesn't reset form state itself -- that's
// still App's job until the form screen is extracted (RFC #19, PR 2).
type enterCompanyFormMsg struct{}

// enterCompanyEditMsg signals that App should switch to the company-edit
// screen for the given company.
type enterCompanyEditMsg struct{ company store.Company }

// selectCompanyMsg signals that App should switch to the posting-list
// screen for the given company.
type selectCompanyMsg struct{ company store.Company }

// Update handles one key press. The returned tea.Cmd (if non-nil) is a
// real async command for App to run through bubbletea as usual. The
// returned tea.Msg (if non-nil) is an intent for App to apply
// synchronously, in the same Update call -- not deferred through
// bubbletea's async loop -- so screen transitions happen with the same
// timing as before this type existed.
func (m *companyListModel) Update(msg tea.KeyMsg, companies []store.Company) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(companies)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "q":
		return tea.Quit, nil
	case msg.String() == "d":
		if m.cursor < len(companies) {
			return deleteCompany(m.store, companies[m.cursor].ID), nil
		}
	case msg.String() == "r":
		if m.cursor < len(companies) {
			c := companies[m.cursor]
			return refreshCompany(m.syncer, c.ID, c.Name), nil
		}
	case msg.String() == "a":
		return nil, enterCompanyFormMsg{}
	case msg.String() == "e":
		if m.cursor < len(companies) {
			return nil, enterCompanyEditMsg{company: companies[m.cursor]}
		}
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(companies) {
			return nil, selectCompanyMsg{company: companies[m.cursor]}
		}
	}
	return nil, nil
}

func (m *companyListModel) View(companies []store.Company, listRows int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Companies") + "\n")
	if len(companies) == 0 {
		b.WriteString("No companies yet. Press 'a' to add one.\n")
	}
	start, end := visibleWindow(m.cursor, len(companies), listRows)
	for i := start; i < end; i++ {
		c := companies[i]
		line := fmt.Sprintf("%s (%s)", c.Name, c.SourceRef)
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view postings  a: add  e: edit  d: delete  r: refresh  q: quit"))
	return b.String()
}

// clampCursor keeps the cursor in bounds after companies shrinks (e.g. a
// deletion). App can't reach m.cursor directly since it's private, so
// this is the explicit hook for that case -- see companyDeletedMsg in
// App.Update.
func (m *companyListModel) clampCursor(n int) {
	if m.cursor >= n && m.cursor > 0 {
		m.cursor = n - 1
	}
}
