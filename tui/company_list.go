package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// companyListModel drives the company-list screen, and owns the companies
// it shows.
type companyListModel struct {
	cursor int

	focused bool

	// showInfo is whether the info box (i) is open. Ephemeral, like cursor:
	// closed each time the app starts.
	showInfo bool
	// searching is whether the '/' prompt is open and taking keys; query
	// is what's been typed into it. Ephemeral, like cursor.
	searching bool
	query     string

	companyService *service.CompanyService
	postingService *service.PostingService

	companies []message.Company
	// openPostings is each company's open, unarchived posting count.
	openPostings map[int64]int

	width    int
	height   int
	instance message.ScreenInstance
}

func newCompanyListModel(companies *service.CompanyService, postings *service.PostingService) *companyListModel {
	return &companyListModel{
		companyService: companies,
		postingService: postings,
		instance:       message.NewScreenInstance(),
	}
}

func (m *companyListModel) Instance() message.ScreenInstance { return m.instance }

func (m *companyListModel) Focused() bool { return m.focused }

func (m *companyListModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.loadRequested),
		model.Always(m.companiesRead),
		model.Always(m.synced),
		model.Always(m.companyChanged),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

// focusChanged reloads when the list comes back, since its open counts can
// change while another screen is up.
func (m *companyListModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	gained := !m.focused && e.Payload.Value.Focused == m.instance
	m.focused = e.Payload.Value.Focused == m.instance
	if gained {
		return m.load()
	}
	return nil
}

func (m *companyListModel) Init() tea.Cmd {
	return m.load()
}

func (m *companyListModel) loadRequested(message.Event[message.Load[message.CompanyList]]) tea.Cmd {
	return m.load()
}

// synced reloads even when the sync failed, since one can fail after
// writing postings.
func (m *companyListModel) synced(message.Event[message.Ran[message.SyncReport]]) tea.Cmd {
	return m.load()
}

// syncCompany asks for c's postings to be fetched from its job board.
func syncCompany(src message.Source, c message.Company) tea.Cmd {
	return func() tea.Msg {
		return message.New(src, message.Subject{Type: message.TypeCompany, ID: c.ID}, message.OpNone, time.Now().UTC(),
			message.Run[message.SyncCompany]{Value: message.SyncCompany{Company: c}})
	}
}

// load reads the active companies along with each one's open posting count
// for the Open column.
func (m *companyListModel) load() tea.Cmd {
	companies, postings, src := m.companyService, m.postingService, message.Source{Instance: m.instance}
	return func() tea.Msg {
		ctx := context.Background()
		active, err := companies.ListActive(ctx)
		if err != nil {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		openPostings, err := postings.CountOpenByCompany(ctx)
		if err != nil {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		list := message.CompanyList{Companies: make([]message.Company, len(active)), OpenPostings: openPostings}
		for i, c := range active {
			list.Companies[i] = companySnapshot(c)
		}
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.CompanyList]{Value: list})
	}
}

func (m *companyListModel) companiesRead(e message.Event[message.Read[message.CompanyList]]) tea.Cmd {
	previous := m.selected()
	m.companies = e.Payload.Value.Companies
	m.openPostings = e.Payload.Value.OpenPostings
	m.keepCursorOn(previous)
	return nil
}

func (m *companyListModel) companyChanged(e message.Event[message.Changed[message.Company]]) tea.Cmd {
	switch e.Meta.Op {
	case message.OpCreated:
		m.companies = append(m.companies, e.Payload.After)
	case message.OpUpdated:
		if i := indexOfCompany(m.companies, e.Meta.Subject.ID); i != -1 {
			m.companies[i] = e.Payload.After
		}
	case message.OpDeleted:
		if i := indexOfCompany(m.companies, e.Meta.Subject.ID); i != -1 {
			m.companies = append(m.companies[:i], m.companies[i+1:]...)
		}
		m.clampCursor(len(m.companies))
		return nil
	}
	sortCompaniesByName(m.companies)
	return nil
}

// backToActiveApplicationsMsg signals that App should switch back to the
// active-applications tab.
type backToActiveApplicationsMsg struct{}

// resize keeps the size it draws in, so it's right when the list is next
// shown.
func (m *companyListModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width, m.height = e.Payload.Value.Width, e.Payload.Value.Rows
	return nil
}

// Update has nothing to do: everything the list handles is registered.
func (m *companyListModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

// keyPressed handles a key. Each command carries a copy of what it needs,
// since it runs off the event loop.
func (m *companyListModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	if m.searching {
		m.updateSearch(key)
		return nil
	}

	// Commands act on the matches a kept search shows, not the whole list.
	visible := m.visible()
	var current message.Company
	onCompany := m.cursor < len(visible)
	if onCompany {
		current = visible[m.cursor]
	}
	src, about := message.Source{Instance: m.instance}, message.Subject{Type: message.TypeCompany, ID: current.ID}
	switch key.String() {
	case "down", "j":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "q":
		return tea.Quit
	case "d":
		if onCompany {
			return message.Push(src, message.ScreenCompanyDelete, about, message.CompanyRow{Company: current, OpenPostings: m.openPostings[current.ID]})
		}
	case "r":
		if onCompany {
			return syncCompany(src, current)
		}
	case "R":
		companies := append([]message.Company(nil), m.companies...)
		return func() tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(),
				message.Run[message.SyncAll]{Value: message.SyncAll{Companies: companies}})
		}
	case "i":
		m.showInfo = !m.showInfo
	case "a":
		return message.Push(src, message.ScreenCompanyForm, message.Subject{}, message.None{})
	case "/":
		m.searching = true
	case "esc":
		m.clearSearch()
	case "e":
		if onCompany {
			return message.Push(src, message.ScreenCompanyEdit, about, current)
		}
	case "enter":
		if onCompany {
			return message.Push(src, message.ScreenCompanyPostings, about, current)
		}
	}
	return nil
}

// updateSearch handles a key while the '/' prompt is open. Every edit
// to the query re-filters straight away and puts the cursor back on the
// first match.
func (m *companyListModel) updateSearch(msg tea.KeyPressMsg) {
	switch msg.String() {
	default:
		if msg.Text != "" {
			m.query += msg.Text
			m.cursor = 0
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor = 0
		} else {
			m.searching = false
		}
	case "esc":
		m.searching = false
		m.clearSearch()
	case "down", "ctrl+n":
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		// Keeps the matches, so letters act on them instead of being typed.
		m.searching = false
	}
}

func (m *companyListModel) paste(msg tea.PasteMsg) tea.Cmd {
	if m.searching {
		m.query += msg.Content
		m.cursor = 0
	}
	return nil
}

// clearSearch drops the query, keeping the highlighted company highlighted
// in the full list.
func (m *companyListModel) clearSearch() {
	cursor := 0
	if visible := m.visible(); m.cursor < len(visible) {
		cursor = max(indexOfCompany(m.companies, visible[m.cursor].ID), 0)
	}
	m.query = ""
	m.cursor = cursor
}

// visible is the part of the companies the query matches: a
// case-insensitive substring of the name, in list order. With no query,
// it's all of them.
func (m *companyListModel) visible() []message.Company {
	if m.query == "" {
		return m.companies
	}
	q := strings.ToLower(m.query)
	var matches []message.Company
	for _, c := range m.companies {
		if strings.Contains(strings.ToLower(c.Name), q) {
			matches = append(matches, c)
		}
	}
	return matches
}

// View renders the list in the rows of the last content size.
func (m *companyListModel) View() tea.View {
	var b strings.Builder
	help := model.HelpStyle.Render("↑/↓ (j/k): select  enter: view postings  /: search  i: info  a: add  e: edit  d: delete  r: refresh  R: sync all  tab: applications  q: quit")
	switch {
	case m.searching:
		help = model.HelpStyle.Render("type to filter  ↑/↓ (ctrl+n/p): select  enter: keep results  esc: clear")
	case m.query != "":
		help = model.HelpStyle.Render("↑/↓ (j/k): select  enter: view postings  /: edit search  esc: clear search  i: info  a: add  e: edit  d: delete  r: refresh  R: sync all  q: quit")
	}
	visible := m.visible()
	// A sync landing while the prompt is open can shrink visible under
	// the cursor.
	cursor := min(m.cursor, max(len(visible)-1, 0))
	var prompt string
	if len(m.companies) > 0 {
		prompt = searchLine(m.searching, m.query, len(visible), len(m.companies))
		b.WriteString(prompt + "\n")
	}
	switch {
	case len(m.companies) == 0:
		b.WriteString("No companies yet. Press 'a' to add one.\n")
	case len(visible) == 0:
		fmt.Fprintf(&b, "No companies match %q.\n", m.query)
	}
	var infoBox string
	if m.showInfo && cursor < len(visible) {
		infoBox = companyInfoBox(visible[cursor], m.width)
	}
	if len(visible) > 0 {
		start, end := visibleWindow(cursor, len(visible), tableRows(m.height, help, infoBox, prompt))
		cursorRow := cursor - start
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("Name", "Open", "Last fetched").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		nameWidth := flexColWidth(m.width, openColWidth, lastFetchedColWidth)
		for i := start; i < end; i++ {
			c := visible[i]
			t.Row(
				padCol(c.Name, nameWidth),
				fmt.Sprintf("%*d", openColWidth, m.openPostings[c.ID]),
				padCol(lastFetchedLabel(c.LastFetchedAt), lastFetchedColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	if infoBox != "" {
		b.WriteString(infoBox + "\n")
	}
	b.WriteString(help)
	return tea.NewView(b.String())
}

// clampCursor keeps the cursor in bounds after the companies shrink (e.g.
// a deletion).
func (m *companyListModel) clampCursor(n int) {
	if m.cursor >= n && m.cursor > 0 {
		m.cursor = n - 1
	}
}

// Company table column widths; Name takes what's left.
const (
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
)

// companyInfoBox renders the info box for c: a header with its name, board
// and slug, then its description word-wrapped. It's always the same height
// -- top and bottom border, the header, and companyInfoDescriptionLines --
// so moving the cursor with the box open never shifts the table.
func companyInfoBox(c message.Company, width int) string {
	// As wide as the table above it, less two border columns and one
	// column of padding on each side.
	textWidth := max(tableWidth(width)-4, 20)

	var lines []string
	if d := strings.Join(strings.Fields(c.Description), " "); d != "" {
		wrapped := lipgloss.NewStyle().Width(textWidth).Render(d)
		for _, line := range strings.Split(wrapped, "\n") {
			lines = append(lines, strings.TrimRight(line, " "))
		}
	} else {
		lines = []string{model.DimStyle.Render("No description yet.")}
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

	header := lipgloss.NewStyle().Bold(true).Render(truncateCol(fmt.Sprintf("%s · %s/%s", c.Name, c.Source, c.SourceRef), textWidth))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(textWidth + 4).
		Render(strings.Join(append([]string{header}, lines...), "\n"))
}

// selected is the ID of the company under the cursor, or 0.
func (m *companyListModel) selected() int64 {
	if visible := m.visible(); m.cursor < len(visible) {
		return visible[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to company id after a reload; if it's
// gone, the cursor stays in range.
func (m *companyListModel) keepCursorOn(id int64) {
	visible := m.visible()
	if i := indexOfCompany(visible, id); i >= 0 {
		m.cursor = i
		return
	}
	m.cursor = min(m.cursor, max(len(visible)-1, 0))
}
