package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/store"
)

// companyListModel drives the company-list screen. It holds only the
// dependencies it needs to dispatch its own commands (store) and
// its own private cursor -- companies themselves are domain data owned
// by App and passed in on every call, never cached here.
type companyListModel struct {
	store  *store.Store
	cursor int
	// showInfo is whether the info box (i) is open. Ephemeral, like cursor:
	// closed each time the app starts.
	showInfo bool
	// searching is whether the '/' prompt is open and taking keys; query
	// is what's been typed into it. Ephemeral, like cursor.
	searching bool
	query     string
}

func newCompanyListModel(s *store.Store) companyListModel {
	return companyListModel{store: s}
}

// enterCompanyFormMsg signals that App should switch to the company-form
// screen. companyListModel doesn't reset form state itself -- that's
// still App's job until the form screen is extracted (RFC #19, PR 2).
type enterCompanyFormMsg struct{}

// enterCompanyEditMsg signals that App should switch to the company-edit
// screen for the given company.
type enterCompanyEditMsg struct{ company store.Company }

// refreshCompanyMsg asks App to refresh the given company. App starts the
// sync itself, rather than this screen, because only App knows whether
// another sync (e.g. a sync-all run) is already under way (#152).
type refreshCompanyMsg struct{ company store.Company }

// syncAllKeyMsg is 'R': App starts a sync-all run (#153).
type syncAllKeyMsg struct{}

// selectCompanyMsg signals that App should switch to the posting-list
// screen for the given company.
type selectCompanyMsg struct{ company store.Company }

// backToActiveApplicationsMsg signals that App should switch back to the
// active-applications screen -- the app's home screen (see decisions.log,
// #43); company list is reached from there via 'c', not the other way
// around, so it needs its own way back.
type backToActiveApplicationsMsg struct{}

// Update handles one key press. The returned tea.Cmd (if non-nil) is a
// real async command for App to run through bubbletea as usual. The
// returned tea.Msg (if non-nil) is an intent for App to apply
// synchronously, in the same Update call -- not deferred through
// bubbletea's async loop -- so screen transitions happen with the same
// timing as before this type existed.
func (m *companyListModel) Update(msg tea.KeyMsg, companies []store.Company) (tea.Cmd, tea.Msg) {
	if m.searching {
		return m.updateSearch(msg, companies)
	}
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
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, backToActiveApplicationsMsg{}
	case msg.String() == "d":
		if m.cursor < len(companies) {
			return deleteCompany(m.store, companies[m.cursor].ID), nil
		}
	case msg.String() == "r":
		if m.cursor < len(companies) {
			return nil, refreshCompanyMsg{company: companies[m.cursor]}
		}
	case msg.String() == "R":
		return nil, syncAllKeyMsg{}
	case msg.String() == "i":
		m.showInfo = !m.showInfo
	case msg.String() == "a":
		return nil, enterCompanyFormMsg{}
	case msg.String() == "/":
		m.searching = true
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

// updateSearch handles a key while the '/' prompt is open. Every edit
// to the query re-filters straight away and puts the cursor back on the
// first match.
func (m *companyListModel) updateSearch(msg tea.KeyMsg, companies []store.Company) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(msg.Runes)
		m.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor = 0
		} else {
			m.searching = false
		}
	case tea.KeyEsc:
		cursor := 0
		if visible := m.visible(companies); m.cursor < len(visible) {
			cursor = max(indexOfCompany(companies, visible[m.cursor].ID), 0)
		}
		m.searching = false
		m.query = ""
		m.cursor = cursor
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.visible(companies))-1 {
			m.cursor++
		}
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyEnter:
		visible := m.visible(companies)
		if m.cursor < len(visible) {
			return nil, selectCompanyMsg{company: visible[m.cursor]}
		}
	}
	return nil, nil
}

// visible is the part of companies the query matches: a case-insensitive
// substring of the name, in the order given. With no query, it's all of
// them.
func (m *companyListModel) visible(companies []store.Company) []store.Company {
	if m.query == "" {
		return companies
	}
	q := strings.ToLower(m.query)
	var matches []store.Company
	for _, c := range companies {
		if strings.Contains(strings.ToLower(c.Name), q) {
			matches = append(matches, c)
		}
	}
	return matches
}

// View renders the list in height terminal rows (App.screenRows).
func (m *companyListModel) View(companies []store.Company, openPostings map[int64]int, width, height int) string {
	var b strings.Builder
	title := titleStyle.Render("Companies")
	help := helpStyle.Render("↑/↓ (j/k): select  enter: view postings  /: search  i: info  a: add  e: edit  d: delete  r: refresh  R: sync all  esc/b: back  q: quit")
	if m.searching {
		help = helpStyle.Render("type to filter  ↑/↓ (ctrl+n/p): select  enter: view postings  esc: clear")
	}
	b.WriteString(title + "\n")
	visible := m.visible(companies)
	// A sync landing while the prompt is open can shrink visible under
	// the cursor.
	cursor := min(m.cursor, max(len(visible)-1, 0))
	var prompt string
	if m.searching {
		prompt = "/" + m.query + "▏  " + dimStyle.Render(fmt.Sprintf("%d of %d", len(visible), len(companies)))
		b.WriteString(prompt + "\n")
	}
	switch {
	case len(companies) == 0:
		b.WriteString("No companies yet. Press 'a' to add one.\n")
	case len(visible) == 0:
		fmt.Fprintf(&b, "No companies match %q.\n", m.query)
	}
	var infoBox string
	if m.showInfo && cursor < len(visible) {
		infoBox = companyInfoBox(visible[cursor], width)
	}
	if len(visible) > 0 {
		start, end := visibleWindow(cursor, len(visible), tableRows(height, title, help, infoBox, prompt))
		cursorRow := cursor - start
		t := table.New().
			Headers("Name", "Open", "Last fetched").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		for i := start; i < end; i++ {
			c := visible[i]
			t.Row(
				padCol(c.Name, companyNameColWidth),
				fmt.Sprintf("%*d", openColWidth, openPostings[c.ID]),
				padCol(lastFetchedLabel(c.LastFetchedAt), lastFetchedColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	if infoBox != "" {
		b.WriteString(infoBox + "\n")
	}
	b.WriteString(help)
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

const (
	companyNameColWidth = 22
	// openColWidth fits a count up to 99999.
	openColWidth = 5
	// lastFetchedColWidth fits "2006-01-02 15:04".
	lastFetchedColWidth = 16
)

// lastFetchedLabel renders when a company was last fetched, in local time,
// or "never" for the zero value.
func lastFetchedLabel(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// padCol truncates s to width columns (see truncateCol) and pads it with
// spaces to exactly that width, so a column keeps the same width however
// long the values currently scrolled into view are.
func padCol(s string, width int) string {
	s = truncateCol(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

const (
	// companyInfoDescriptionLines is how many wrapped description lines the
	// info box shows; longer descriptions end with an ellipsis.
	companyInfoDescriptionLines = 4
	// defaultCompanyInfoWidth is the box's text width before the terminal
	// reports its size; maxCompanyInfoWidth keeps lines readable on very
	// wide terminals.
	defaultCompanyInfoWidth = 76
	maxCompanyInfoWidth     = 96
)

// companyInfoBox renders the info box for c: a header with its name, board
// and slug, then its description word-wrapped. It's always the same height
// -- top and bottom border, the header, and companyInfoDescriptionLines --
// so moving the cursor with the box open never shifts the table.
func companyInfoBox(c store.Company, width int) string {
	textWidth := defaultCompanyInfoWidth
	if width > 0 {
		// Two border columns and one column of padding on each side.
		textWidth = min(max(width-4, 20), maxCompanyInfoWidth)
	}

	var lines []string
	if d := strings.Join(strings.Fields(c.Description), " "); d != "" {
		wrapped := lipgloss.NewStyle().Width(textWidth).Render(d)
		for _, line := range strings.Split(wrapped, "\n") {
			lines = append(lines, strings.TrimRight(line, " "))
		}
	} else {
		lines = []string{dimStyle.Render("No description yet.")}
	}
	if len(lines) > companyInfoDescriptionLines {
		last := []rune(lines[companyInfoDescriptionLines-1])
		if len(last) > textWidth-1 {
			last = last[:textWidth-1]
		}
		lines = append(lines[:companyInfoDescriptionLines-1], string(last)+"…")
	}
	for len(lines) < companyInfoDescriptionLines {
		lines = append(lines, "")
	}

	// Bold without titleStyle's MarginBottom, which would add a line and break
	// the box's fixed height.
	header := lipgloss.NewStyle().Bold(true).Render(truncateCol(fmt.Sprintf("%s · %s/%s", c.Name, c.Source, c.SourceRef), textWidth))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(textWidth + 2).
		Render(strings.Join(append([]string{header}, lines...), "\n"))
}
