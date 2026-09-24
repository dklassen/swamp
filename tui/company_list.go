package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

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
	// showInfo is whether the info box (i) is open. Ephemeral, like cursor:
	// closed each time the app starts.
	showInfo bool
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
			c := companies[m.cursor]
			return refreshCompany(m.syncer, c.ID, c.Name), nil
		}
	case msg.String() == "i":
		m.showInfo = !m.showInfo
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

func (m *companyListModel) View(companies []store.Company, openPostings map[int64]int, width, listRows int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Companies") + "\n")
	if len(companies) == 0 {
		b.WriteString("No companies yet. Press 'a' to add one.\n")
	}
	var infoBox string
	if m.showInfo && m.cursor < len(companies) {
		infoBox = companyInfoBox(companies[m.cursor], width)
	}
	if len(companies) > 0 {
		// Same table chrome as the posting list, so the same line budget.
		rows := listRows - postingTableChromeLines
		if infoBox != "" {
			rows -= companyInfoBoxHeight
		}
		if rows < 0 {
			rows = 0
		}
		start, end := visibleWindow(m.cursor, len(companies), rows)
		cursorRow := m.cursor - start
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
			c := companies[i]
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
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view postings  i: info  a: add  e: edit  d: delete  r: refresh  esc/b: back  q: quit"))
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
	// companyInfoBoxHeight is the info box's fixed height: top and bottom
	// border, a header line, and the description lines. Fixed so moving the
	// cursor with the box open never shifts the table.
	companyInfoBoxHeight = 2 + 1 + companyInfoDescriptionLines
	// defaultCompanyInfoWidth is the box's text width before the terminal
	// reports its size; maxCompanyInfoWidth keeps lines readable on very
	// wide terminals.
	defaultCompanyInfoWidth = 76
	maxCompanyInfoWidth     = 96
)

// companyInfoBox renders the info box for c: a header with its name, board
// and slug, then its description word-wrapped, always companyInfoBoxHeight
// lines tall.
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
