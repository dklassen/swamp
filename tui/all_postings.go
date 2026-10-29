package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// seenColWidth fits a first-seen date, "Sep 30".
const seenColWidth = 6

// fitColWidth fits the column's header, "Fit"; a score is one digit.
const fitColWidth = 3

const allPostingsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  s: sort  enter: detail  i: interested  x: archive  o: open  tab: applications"

const allPostingsSearchHelp = "type to search  ↑/↓ (ctrl+n/p): select  ←/→: page  enter: keep results  esc: clear"

// allPostingsResultsHelp is the help once enter has kept a search's results.
const allPostingsResultsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  s: sort  enter: detail  i: interested  x: archive  o: open  /: edit search  esc: clear search"

// allPostingsModel drives the all-postings view: every company's open
// postings that its filters let through, newest to Swamp first, a page at
// a time. Pages are fixed rather than scrolling, so a posting arriving
// lands on page 1 instead of shifting the one being read.
type allPostingsModel struct {
	postings *service.PostingService
	cursor   int
	// starts is where each page so far began, page 1's being the zero
	// start; the last is the page on screen.
	starts []pageStart
	// order is newest first or best fit first.
	order posting.Order
	page  posting.SearchPage
	// size is the number of rows page was read at.
	size int
	// searching is whether the '/' line is open and taking keys; query is
	// what's been typed into it, kept with its results once enter closes it.
	searching bool
	query     string
	// typed counts keys typed into the search, so only the pause after the
	// latest one searches.
	typed int
	// total is how many postings there are without the query.
	total int
}

func newAllPostingsModel(postings *service.PostingService) allPostingsModel {
	return allPostingsModel{postings: postings, starts: []pageStart{{}}, order: posting.OrderIDDesc}
}

// pageStart is the row a page was read after: its ID, and its score for the
// order by fit.
type pageStart struct {
	id    int64
	score int
}

type allPostingsLoadedMsg struct {
	start pageStart
	order posting.Order
	query string
	size  int
	page  posting.SearchPage
	total int
	err   error
}

// searchPause is how long typing must stop before the search runs, so fast
// typing doesn't queue a load per key.
const searchPause = 150 * time.Millisecond

// allPostingsSearchMsg is the pause after the typed'th key.
type allPostingsSearchMsg struct{ typed int }

// pageSize is how many rows fill height: what's left after the search and
// page lines, the table's chrome and the help.
func (m *allPostingsModel) pageSize(height int) int {
	return max(tableRows(height-2, model.HelpStyle.Render(allPostingsHelp)), 1)
}

// load reads the page on screen again, size rows long, with the query and,
// when there is one, the total without it.
func (m *allPostingsModel) load(size int) tea.Cmd {
	s, start, order, query := m.postings, m.starts[len(m.starts)-1], m.order, m.query
	return func() tea.Msg {
		ctx := context.Background()
		open := posting.ListingOpen
		page, err := s.Search(ctx, posting.Search{
			ListingStatus: &open,
			Order:         order,
			AfterID:       start.id,
			AfterScore:    start.score,
			Query:         query,
			Limit:         size,
		})
		if err != nil {
			return allPostingsLoadedMsg{start: start, order: order, query: query, size: size, err: err}
		}
		total := page.Total
		if query != "" {
			all, err := s.Search(ctx, posting.Search{ListingStatus: &open, Limit: 1})
			if err != nil {
				return allPostingsLoadedMsg{start: start, order: order, query: query, size: size, err: err}
			}
			total = all.Total
		}
		return allPostingsLoadedMsg{start: start, order: order, query: query, size: size, page: *page, total: total}
	}
}

// loaded shows msg's page, if it's still the page on screen. A page past
// the first that has emptied, its postings archived or closed, steps back.
func (m *allPostingsModel) loaded(msg allPostingsLoadedMsg) tea.Cmd {
	if msg.start != m.starts[len(m.starts)-1] || msg.order != m.order || msg.query != m.query {
		return nil
	}
	if len(msg.page.Listings) == 0 && len(m.starts) > 1 {
		m.starts = m.starts[:len(m.starts)-1]
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
	m.starts = []pageStart{{}}
	m.cursor = 0
	return m.load(size)
}

func (m *allPostingsModel) nextPage(size int) tea.Cmd {
	n := len(m.page.Listings)
	if n == 0 || !m.page.HasMore {
		return nil
	}
	last := m.page.Listings[n-1]
	m.starts = append(m.starts, pageStart{id: last.ID, score: last.Score})
	m.cursor = 0
	return m.load(size)
}

func (m *allPostingsModel) previousPage(size int) tea.Cmd {
	if len(m.starts) == 1 {
		return nil
	}
	m.starts = m.starts[:len(m.starts)-1]
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

func (m *allPostingsModel) paste(msg tea.PasteMsg) tea.Cmd {
	if !m.searching {
		return nil
	}
	return m.typedKey(m.query + msg.Content)
}

// orderLabel names the order the pages are in.
func (m *allPostingsModel) orderLabel() string {
	if m.order == posting.OrderScoreDesc {
		return "best fit first"
	}
	return "newest first"
}

// pages is how many pages Total fills at the page size.
func (m *allPostingsModel) pages() int {
	if m.size <= 0 {
		return 1
	}
	return max((m.page.Total+m.size-1)/m.size, 1)
}

// fitCell is a posting's current score, blank when it has none and dimmed
// when its content has changed since.
func fitCell(l posting.Listing) string {
	if l.Score == 0 {
		return padCol("", fitColWidth)
	}
	cell := padCol(fmt.Sprint(l.Score), fitColWidth)
	if l.ScoreStale {
		return model.DimStyle.Render(cell)
	}
	return cell
}

// locationLabel is a posting's location with its workplace type, when the
// board gave one.
func locationLabel(l posting.Listing) string {
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
	help := model.HelpStyle.Render(allPostingsHelp)
	switch {
	case m.searching:
		help = model.HelpStyle.Render(allPostingsSearchHelp)
	case m.query != "":
		help = model.HelpStyle.Render(allPostingsResultsHelp)
	}
	b.WriteString(searchLine(m.searching, m.query, m.page.Total, m.total) + "\n")
	fmt.Fprintf(&b, "%s\n", model.DimStyle.Render(fmt.Sprintf("Page %d of %d · %d postings · %s", len(m.starts), m.pages(), m.page.Total, m.orderLabel())))
	switch {
	case len(m.page.Listings) == 0 && m.query != "":
		fmt.Fprintf(&b, "No postings match %q.\n", m.query)
	case len(m.page.Listings) == 0:
		b.WriteString("No open postings.\n")
	default:
		// A banner can take rows after the page was read, so draw what fits.
		start, end := visibleWindow(m.cursor, len(m.page.Listings), tableRows(height-2, help))
		cursorRow := m.cursor - start
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("Seen", "Company", "Title", "Department", "Location", "", "Fit").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(width, seenColWidth, homeCompanyColWidth, departmentColWidth, locationColWidth, markerColWidth, fitColWidth)
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
				fitCell(l),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return b.String()
}

// Update handles a key on the all-postings view, given the page size that
// fills the window now.
func (m *allPostingsModel) Update(msg tea.KeyPressMsg, size int) (tea.Cmd, tea.Msg) {
	if m.searching {
		return m.updateSearch(msg, size)
	}
	switch {
	case msg.String() == "down", msg.String() == "j":
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case msg.String() == "up", msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "right", msg.String() == "l":
		return m.nextPage(size), nil
	case msg.String() == "enter":
		if m.cursor < len(m.page.Listings) {
			return nil, message.PostingDetail{Posting: message.Posting{ID: m.page.Listings[m.cursor].ID}}
		}
	case msg.String() == "s":
		if m.order == posting.OrderScoreDesc {
			m.order = posting.OrderIDDesc
		} else {
			m.order = posting.OrderScoreDesc
		}
		return m.firstPage(size), nil
	case msg.String() == "/":
		m.searching = true
	case msg.String() == "esc":
		return m.clearSearch(size), nil
	case msg.String() == "i":
		if m.cursor < len(m.page.Listings) {
			l := m.page.Listings[m.cursor]
			return toggleInterested(m.postings, message.Source{}, l.ID, l.Interested), nil
		}
	case msg.String() == "x":
		if m.cursor < len(m.page.Listings) {
			l := m.page.Listings[m.cursor]
			return toggleArchived(m.postings, message.Source{}, l.ID, l.Archived), nil
		}
	case msg.String() == "o":
		if m.cursor < len(m.page.Listings) && m.page.Listings[m.cursor].JobURL != "" {
			return nil, openJobURLMsg{id: m.page.Listings[m.cursor].ID, url: m.page.Listings[m.cursor].JobURL}
		}
	case msg.String() == "left", msg.String() == "h":
		return m.previousPage(size), nil
	}
	return nil, nil
}

// updateSearch handles a key while the search line is open: letters go
// into the query, so only keys that aren't text move or page.
func (m *allPostingsModel) updateSearch(msg tea.KeyPressMsg, size int) (tea.Cmd, tea.Msg) {
	switch msg.String() {
	default:
		if msg.Text != "" {
			return m.typedKey(m.query + msg.Text), nil
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			return m.typedKey(string(r[:len(r)-1])), nil
		}
		m.searching = false
	case "esc":
		m.searching = false
		return m.clearSearch(size), nil
	case "down", "ctrl+n":
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right":
		return m.nextPage(size), nil
	case "left":
		return m.previousPage(size), nil
	case "enter":
		// Keeps the results, so letters act on them instead of being typed.
		m.searching = false
	}
	return nil, nil
}

// clearSearch drops the query and reloads page 1 without it, if there was one.
func (m *allPostingsModel) clearSearch(size int) tea.Cmd {
	if m.query == "" {
		return nil
	}
	m.query = ""
	m.typed++
	return m.firstPage(size)
}

// openJobURLMsg asks App to open a posting in the browser.
type openJobURLMsg struct {
	id  int64
	url string
}

// allPostingsDetailLoadedMsg is the posting enter opened: a page row
// lacks the description the detail shows.
type allPostingsDetailLoadedMsg struct {
	posting posting.Posting
	err     error
}

func loadAllPostingsDetail(postings *service.PostingService, id int64) tea.Cmd {
	return func() tea.Msg {
		p, err := postings.Get(context.Background(), id)
		if err != nil {
			return allPostingsDetailLoadedMsg{err: err}
		}
		return allPostingsDetailLoadedMsg{posting: *p}
	}
}
