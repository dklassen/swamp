package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/store"
)

// Column widths for the posting table; Title takes what's left. Values
// longer than these get truncated with an ellipsis rather than wrapped, so
// every row stays exactly one physical line (visibleWindow's cursor/scroll
// math assumes one line per posting).
const (
	markerColWidth     = 1
	departmentColWidth = 18
	locationColWidth   = 20
	statusColWidth     = 8
)

// postingTableChromeLines is the number of physical lines lipgloss/table's
// default border adds beyond one line per data row: top border, header,
// header separator, bottom border.
const postingTableChromeLines = 4

// tableRows is how many data rows a table screen can show in height
// terminal rows: what's left once the table's own chrome and everything
// else the screen draws around it (help, notices, ...) have taken
// theirs. Each of around is measured as rendered, margins and all, so a
// screen can't outgrow the terminal by drawing a line it forgot to count
// (issue #138). An empty string is something the screen isn't drawing
// this time, so it takes no rows.
func tableRows(height int, around ...string) int {
	rows := height - postingTableChromeLines
	for _, s := range around {
		if s != "" {
			rows -= lipgloss.Height(s)
		}
	}
	return max(rows, 0)
}

// truncateCol shortens s to at most max columns wide, replacing the tail
// with an ellipsis when it doesn't fit. Width-aware (not byte-aware) so
// multi-byte runes truncate correctly.
func truncateCol(s string, max int) string {
	return ansi.TruncateWc(s, max, "…")
}

// postingMarker renders a posting's markup state as a single-character
// column for the posting list. Archived takes precedence over interested
// when both are set, matching the same precedence the 00003 migration's
// Down path uses when collapsing both flags back into one enum value.
func postingMarker(m store.PostingMarkup) string {
	switch {
	case m.ArchivedAt != nil:
		return "✕"
	case m.InterestedAt != nil:
		return "★"
	default:
		return " "
	}
}

// filterSummaryLine renders the currently-active filters as a one-line
// summary (e.g. "Filtering: Department: Engineering | Location:
// Remote"), or "" if no filters are active -- so the filter state isn't
// invisible in the posting list.
func filterSummaryLine(departments, locations []string) string {
	if len(departments) == 0 && len(locations) == 0 {
		return ""
	}
	var parts []string
	if len(departments) > 0 {
		parts = append(parts, "Department: "+strings.Join(departments, ", "))
	}
	if len(locations) > 0 {
		parts = append(parts, "Location: "+strings.Join(locations, ", "))
	}
	return "Filtering: " + strings.Join(parts, " | ")
}

// postingListModel drives the posting-list screen. It holds only the
// dependency it needs to dispatch its own commands (store) and its own
// private cursor -- postings/markup/hideArchived/active filters are
// domain data owned by App, passed in as a postingListSnapshot on every
// call, never cached here.
type postingListModel struct {
	store  *store.Store
	cursor int
}

func newPostingListModel(s *store.Store) postingListModel {
	return postingListModel{store: s}
}

// postingListSnapshot is the read-only domain data App hands in on every
// call.
type postingListSnapshot struct {
	companyDescription      string
	postings                []store.Posting
	markup                  map[int64]store.PostingMarkup
	hideArchived            bool
	activeFilterDepartments []string
	activeFilterLocations   []string
}

// backToCompanyListMsg signals that App should switch to the
// company-list screen.
type backToCompanyListMsg struct{}

// enterPostingDetailMsg signals that App should switch to the
// posting-detail screen for the given posting.
type enterPostingDetailMsg struct{ postingID int64 }

// enterFilterSelectMsg signals that App should switch to the
// filter-select screen.
type enterFilterSelectMsg struct{}

// refreshSelectedCompanyMsg asks App to re-sync the company whose
// postings are on screen. App starts the sync, since only it knows whether
// a sync-all run is already under way.
type refreshSelectedCompanyMsg struct{}

// toggleHideArchivedMsg signals that App should flip hideArchived and
// reload postings -- hideArchived is App-owned domain state, not this
// screen's to mutate directly.
type toggleHideArchivedMsg struct{}

func (m *postingListModel) Update(msg tea.KeyMsg, snap postingListSnapshot) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(snap.postings)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyEsc, msg.String() == "b":
		return nil, backToCompanyListMsg{}
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(snap.postings) {
			return nil, enterPostingDetailMsg{postingID: snap.postings[m.cursor].ID}
		}
	case msg.String() == "o":
		if m.cursor < len(snap.postings) {
			if url := snap.postings[m.cursor].JobURL; url != "" {
				return openInBrowser(url), nil
			}
		}
	case msg.String() == "r":
		return nil, refreshSelectedCompanyMsg{}
	case msg.String() == "f":
		return nil, enterFilterSelectMsg{}
	case msg.String() == "i":
		if m.cursor < len(snap.postings) {
			p := snap.postings[m.cursor]
			return toggleInterested(m.store, p.ID, snap.markup[p.ID].InterestedAt != nil), nil
		}
	case msg.String() == "x":
		if m.cursor < len(snap.postings) {
			p := snap.postings[m.cursor]
			return toggleArchived(m.store, p.ID, snap.markup[p.ID].ArchivedAt != nil), nil
		}
	case msg.String() == "A":
		return nil, toggleHideArchivedMsg{}
	}
	return nil, nil
}

// View renders the list in height terminal rows (App.screenRows). Every
// line drawn above or below the table -- the description, filter summary
// and archived notice included -- costs the table a row, so the list
// still fits.
func (m *postingListModel) View(snap postingListSnapshot, width, height int) string {
	var b strings.Builder
	help := helpStyle.Render("↑/↓ (j/k): select  enter: view detail  o: open in browser  r: refresh  f: filters  i: interested  x: archive  A: toggle archived visibility  esc/b: back")
	// The description gets one line, truncated to the table's width.
	var description, summary, archived string
	if snap.companyDescription != "" {
		description = dimStyle.Render(truncateCol(strings.Join(strings.Fields(snap.companyDescription), " "), tableWidth(width)))
	}
	if s := filterSummaryLine(snap.activeFilterDepartments, snap.activeFilterLocations); s != "" {
		summary = helpStyle.Render(s)
	}
	if snap.hideArchived {
		archived = helpStyle.Render("Archived postings hidden (press 'A' to show)")
	}
	for _, line := range []string{description, summary, archived} {
		if line != "" {
			b.WriteString(line + "\n")
		}
	}
	if len(snap.postings) == 0 {
		b.WriteString("No postings yet. Press 'r' to refresh.\n")
	} else {
		start, end := visibleWindow(m.cursor, len(snap.postings), tableRows(height, description, summary, archived, help))
		cursorRow := m.cursor - start
		t := table.New().
			Headers("", "Title", "Department", "Location", "Status").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(width, markerColWidth, departmentColWidth, locationColWidth, statusColWidth)
		for i := start; i < end; i++ {
			p := snap.postings[i]
			t.Row(
				padCol(postingMarker(snap.markup[p.ID]), markerColWidth),
				padCol(p.Title, titleWidth),
				padCol(p.Department, departmentColWidth),
				padCol(p.Location, locationColWidth),
				padCol(p.ListingStatus, statusColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return b.String()
}

// resetCursor points the cursor back at the top of the list -- called
// when a fresh set of postings loads (see postingsLoadedMsg in
// App.Update).
func (m *postingListModel) resetCursor() {
	m.cursor = 0
}

// clampCursor keeps the cursor in bounds after postings shrinks (e.g.
// archiving while hideArchived is on). App can't reach m.cursor directly
// since it's private, so this is the explicit hook for that case.
func (m *postingListModel) clampCursor(n int) {
	if m.cursor >= n && m.cursor > 0 {
		m.cursor = n - 1
	}
}

// resetCursorIfOutOfBounds resets the cursor to the top if it's no
// longer a valid index into n postings -- distinct from clampCursor
// (which lands on the last valid index): used after a filter save
// re-narrows the list, where landing back at the top reads more
// naturally than landing on whatever the last item happens to be.
func (m *postingListModel) resetCursorIfOutOfBounds(n int) {
	if m.cursor >= n {
		m.resetCursor()
	}
}

// setCursor points the cursor at index i, if valid. Used to keep this
// screen's cursor in sync with wherever h/l navigation inside
// postingDetailModel last landed, when the user returns to the list --
// the two models don't share a cursor (postingDetailModel tracks only
// the one posting it's showing), so App reconciles them here at the
// transition boundary rather than either model needing to know about
// the other (see backToPostingListMsg in App.Update).
func (m *postingListModel) setCursor(i int) {
	if i >= 0 {
		m.cursor = i
	}
}

// selected is the ID of the posting under the cursor, or 0.
func (m *postingListModel) selected(postings []store.Posting) int64 {
	if m.cursor < len(postings) {
		return postings[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to posting id after a reload in place; if
// it's gone, the cursor stays in range.
func (m *postingListModel) keepCursorOn(id int64, postings []store.Posting) {
	if i := indexOfPosting(postings, id); i >= 0 {
		m.cursor = i
		return
	}
	m.clampCursor(len(postings))
}
