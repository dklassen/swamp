package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/store"
)

// seenColWidth fits a first-seen date, "Sep 30".
const seenColWidth = 6

const allPostingsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  enter: detail  i: interested  x: archive  o: open  tab: applications"

// allPostingsModel drives the all-postings view: every company's open
// postings that its filters let through, newest to Swamp first, a page at
// a time. Pages are fixed rather than scrolling, so a posting arriving
// lands on page 1 instead of shifting the one being read.
type allPostingsModel struct {
	store  *store.Store
	cursor int
	// afterIDs is the cursor each page so far was read after, page 1's
	// being 0; the last is the page on screen.
	afterIDs []int64
	page     store.PostingSearchPage
	// size is the number of rows page was read at.
	size int
}

func newAllPostingsModel(s *store.Store) allPostingsModel {
	return allPostingsModel{store: s, afterIDs: []int64{0}}
}

type allPostingsLoadedMsg struct {
	afterID int64
	size    int
	page    store.PostingSearchPage
	err     error
}

// pageSize is how many rows fill height: what's left after the page line,
// the table's chrome and the help.
func (m *allPostingsModel) pageSize(height int) int {
	return max(tableRows(height-1, helpStyle.Render(allPostingsHelp)), 1)
}

// load reads the page on screen again, size rows long.
func (m *allPostingsModel) load(size int) tea.Cmd {
	s, afterID := m.store, m.afterIDs[len(m.afterIDs)-1]
	return func() tea.Msg {
		page, err := s.SearchPostings(context.Background(), store.PostingSearch{
			Order:   store.PostingOrderIDDesc,
			AfterID: afterID,
			Limit:   size,
		})
		return allPostingsLoadedMsg{afterID: afterID, size: size, page: page, err: err}
	}
}

// loaded shows msg's page, if it's still the page on screen.
func (m *allPostingsModel) loaded(msg allPostingsLoadedMsg) {
	if msg.afterID != m.afterIDs[len(m.afterIDs)-1] {
		return
	}
	m.page, m.size = msg.page, msg.size
	m.cursor = min(m.cursor, max(len(m.page.Listings)-1, 0))
}

// pages is how many pages Total fills at the page size.
func (m *allPostingsModel) pages() int {
	if m.size <= 0 {
		return 1
	}
	return max((m.page.Total+m.size-1)/m.size, 1)
}

// locationLabel is a posting's location with its workplace type, when the
// board gave one.
func locationLabel(l store.PostingListing) string {
	switch {
	case l.WorkplaceType == "":
		return l.Location
	case l.Location == "":
		return l.WorkplaceType
	}
	return l.Location + " (" + l.WorkplaceType + ")"
}

func (m *allPostingsModel) View(width, height int) string {
	var b strings.Builder
	help := helpStyle.Render(allPostingsHelp)
	fmt.Fprintf(&b, "%s\n", dimStyle.Render(fmt.Sprintf("Page %d of %d · %d postings", len(m.afterIDs), m.pages(), m.page.Total)))
	if len(m.page.Listings) == 0 {
		b.WriteString("No open postings.\n")
	} else {
		cursorRow := m.cursor
		t := table.New().
			Headers("Seen", "Company", "Title", "Department", "Location", "").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(width, seenColWidth, homeCompanyColWidth, departmentColWidth, locationColWidth, markerColWidth)
		for _, l := range m.page.Listings {
			marker := " "
			if l.Interested {
				marker = "★"
			}
			t.Row(
				padCol(l.FirstSeenAt.Local().Format("Jan 2"), seenColWidth),
				padCol(l.CompanyName, homeCompanyColWidth),
				padCol(l.Title, titleWidth),
				padCol(l.Department, departmentColWidth),
				padCol(locationLabel(l), locationColWidth),
				padCol(marker, markerColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return b.String()
}
