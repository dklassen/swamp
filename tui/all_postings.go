package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/store"
)

// seenColWidth fits a first-seen date, "Sep 30".
const seenColWidth = 6

const allPostingsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  enter: detail  i: interested  x: archive  o: open  tab: applications"

const allPostingsSearchHelp = "type to search  ↑/↓ (ctrl+n/p): select  ←/→: page  enter: detail  esc: clear"

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
	// searching is whether the '/' line is open and taking keys; query is
	// what's been typed into it.
	searching bool
	query     string
	// typed counts keys typed into the search, so only the pause after the
	// latest one searches.
	typed int
	// total is how many postings there are without the query.
	total int
}

func newAllPostingsModel(s *store.Store) allPostingsModel {
	return allPostingsModel{store: s, afterIDs: []int64{0}}
}

type allPostingsLoadedMsg struct {
	afterID int64
	query   string
	size    int
	page    store.PostingSearchPage
	total   int
	err     error
}

// searchPause is how long typing must stop before the search runs, so fast
// typing doesn't queue a load per key.
const searchPause = 150 * time.Millisecond

// allPostingsSearchMsg is the pause after the typed'th key.
type allPostingsSearchMsg struct{ typed int }

// pageSize is how many rows fill height: what's left after the search and
// page lines, the table's chrome and the help.
func (m *allPostingsModel) pageSize(height int) int {
	return max(tableRows(height-2, helpStyle.Render(allPostingsHelp)), 1)
}

// load reads the page on screen again, size rows long, with the query and,
// when there is one, the total without it.
func (m *allPostingsModel) load(size int) tea.Cmd {
	s, afterID, query := m.store, m.afterIDs[len(m.afterIDs)-1], m.query
	return func() tea.Msg {
		ctx := context.Background()
		page, err := s.SearchPostings(ctx, store.PostingSearch{
			Order:   store.PostingOrderIDDesc,
			AfterID: afterID,
			Query:   query,
			Limit:   size,
		})
		total := page.Total
		if err == nil && query != "" {
			var all store.PostingSearchPage
			all, err = s.SearchPostings(ctx, store.PostingSearch{Limit: 1})
			total = all.Total
		}
		return allPostingsLoadedMsg{afterID: afterID, query: query, size: size, page: page, total: total, err: err}
	}
}

// loaded shows msg's page, if it's still the page on screen. A page past
// the first that has emptied, its postings archived or closed, steps back.
func (m *allPostingsModel) loaded(msg allPostingsLoadedMsg) tea.Cmd {
	if msg.afterID != m.afterIDs[len(m.afterIDs)-1] || msg.query != m.query {
		return nil
	}
	if len(msg.page.Listings) == 0 && len(m.afterIDs) > 1 {
		m.afterIDs = m.afterIDs[:len(m.afterIDs)-1]
		return m.load(msg.size)
	}
	m.page, m.size, m.total = msg.page, msg.size, msg.total
	m.cursor = min(m.cursor, max(len(m.page.Listings)-1, 0))
	return nil
}

// searched reads page 1 with the query, unless a key was typed after the
// one msg's pause followed.
func (m *allPostingsModel) searched(msg allPostingsSearchMsg, size int) tea.Cmd {
	if msg.typed != m.typed {
		return nil
	}
	return m.firstPage(size)
}

func (m *allPostingsModel) firstPage(size int) tea.Cmd {
	m.afterIDs = []int64{0}
	m.cursor = 0
	return m.load(size)
}

func (m *allPostingsModel) nextPage(size int) tea.Cmd {
	n := len(m.page.Listings)
	if n == 0 || !m.page.HasMore {
		return nil
	}
	m.afterIDs = append(m.afterIDs, m.page.Listings[n-1].ID)
	m.cursor = 0
	return m.load(size)
}

func (m *allPostingsModel) previousPage(size int) tea.Cmd {
	if len(m.afterIDs) == 1 {
		return nil
	}
	m.afterIDs = m.afterIDs[:len(m.afterIDs)-1]
	m.cursor = 0
	return m.load(size)
}

// typedKey changes the query and waits for a pause before searching.
func (m *allPostingsModel) typedKey(query string) tea.Cmd {
	m.query = query
	m.typed++
	typed := m.typed
	return tea.Tick(searchPause, func(time.Time) tea.Msg { return allPostingsSearchMsg{typed: typed} })
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
	if m.searching {
		help = helpStyle.Render(allPostingsSearchHelp)
	}
	b.WriteString(searchLine(m.searching, m.query, m.page.Total, m.total) + "\n")
	fmt.Fprintf(&b, "%s\n", dimStyle.Render(fmt.Sprintf("Page %d of %d · %d postings", len(m.afterIDs), m.pages(), m.page.Total)))
	if len(m.page.Listings) == 0 {
		b.WriteString("No open postings.\n")
	} else {
		// A banner can take rows after the page was read, so draw what fits.
		start, end := visibleWindow(m.cursor, len(m.page.Listings), tableRows(height-2, help))
		cursorRow := m.cursor - start
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
		for _, l := range m.page.Listings[start:end] {
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

// Update handles a key on the all-postings view, given the page size that
// fills the window now.
func (m *allPostingsModel) Update(msg tea.KeyMsg, size int) (tea.Cmd, tea.Msg) {
	if m.searching {
		return m.updateSearch(msg, size)
	}
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyRight, msg.String() == "l":
		return m.nextPage(size), nil
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(m.page.Listings) {
			return nil, enterPostingDetailMsg{postingID: m.page.Listings[m.cursor].ID}
		}
	case msg.String() == "/":
		m.searching = true
	case msg.String() == "i":
		if m.cursor < len(m.page.Listings) {
			l := m.page.Listings[m.cursor]
			return toggleInterested(m.store, l.ID, l.Interested), nil
		}
	case msg.String() == "x":
		if m.cursor < len(m.page.Listings) {
			l := m.page.Listings[m.cursor]
			return toggleArchived(m.store, l.ID, l.Archived), nil
		}
	case msg.String() == "o":
		if m.cursor < len(m.page.Listings) && m.page.Listings[m.cursor].JobURL != "" {
			return nil, openJobURLMsg{url: m.page.Listings[m.cursor].JobURL}
		}
	case msg.Type == tea.KeyLeft, msg.String() == "h":
		return m.previousPage(size), nil
	}
	return nil, nil
}

// updateSearch handles a key while the search line is open: letters go
// into the query, so only keys that aren't text move or page.
func (m *allPostingsModel) updateSearch(msg tea.KeyMsg, size int) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		return m.typedKey(m.query + string(msg.Runes)), nil
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			return m.typedKey(string(r[:len(r)-1])), nil
		}
		m.searching = false
	case tea.KeyEsc:
		m.searching = false
		if m.query != "" {
			m.query = ""
			m.typed++
			return m.firstPage(size), nil
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight:
		return m.nextPage(size), nil
	case tea.KeyLeft:
		return m.previousPage(size), nil
	case tea.KeyEnter:
		if m.cursor < len(m.page.Listings) {
			return nil, enterPostingDetailMsg{postingID: m.page.Listings[m.cursor].ID}
		}
	}
	return nil, nil
}

// openJobURLMsg asks App to open a posting in the browser.
type openJobURLMsg struct{ url string }

// allPostingsDetailLoadedMsg is the posting enter opened: a page row
// lacks the description the detail shows.
type allPostingsDetailLoadedMsg struct {
	posting store.Posting
	err     error
}

func loadAllPostingsDetail(s *store.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		p, err := s.GetPosting(context.Background(), id)
		return allPostingsDetailLoadedMsg{posting: p, err: err}
	}
}
