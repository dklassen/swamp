package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

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
	companyName             string
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
			url := snap.postings[m.cursor].JobURL
			if url != nil && *url != "" {
				return openInBrowser(*url), nil
			}
		}
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

func (m *postingListModel) View(snap postingListSnapshot, listRows int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Postings: %s", snap.companyName)) + "\n")
	if summary := filterSummaryLine(snap.activeFilterDepartments, snap.activeFilterLocations); summary != "" {
		b.WriteString(helpStyle.Render(summary) + "\n")
	}
	if snap.hideArchived {
		b.WriteString(helpStyle.Render("Archived postings hidden (press 'A' to show)") + "\n")
	}
	if len(snap.postings) == 0 {
		b.WriteString("No postings yet. Press 'r' from the company list to refresh.\n")
	}
	start, end := visibleWindow(m.cursor, len(snap.postings), listRows)
	for i := start; i < end; i++ {
		p := snap.postings[i]
		marker := postingMarker(snap.markup[p.ID])
		line := fmt.Sprintf("%s %s | %s | %s | %s", marker, p.Title, derefOr(p.Department, ""), derefOr(p.Location, ""), p.ListingStatus)
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view detail  o: open in browser  f: filters  i: interested  x: archive  A: toggle archived visibility  esc/b: back"))
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
